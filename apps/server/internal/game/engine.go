package game

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"gamematch/internal/config"
	"gamematch/internal/store"
)

// Engine drives game sessions. A single mutex serializes state transitions,
// which is sufficient for this single-process prototype.
type Engine struct {
	Store *store.Store
	Cfg   config.Config
	mu    sync.Mutex
}

// NewEngine builds an engine.
func NewEngine(s *store.Store, cfg config.Config) *Engine {
	return &Engine{Store: s, Cfg: cfg}
}

// Sentinel errors mapped to HTTP codes by the API layer.
var (
	ErrForbidden   = errors.New("forbidden")
	ErrClosed      = errors.New("closed")
	ErrNotFound    = errors.New("not found")
	ErrNotYourTurn = errors.New("not your turn")
)

const (
	modePractice  = "practice"
	practiceAfter = 30 * time.Second
	metroExpand   = 32 * time.Second
	pollStale     = 15 * time.Second
	pendingTTL    = 30 * time.Second
	revealWindow  = 4 * time.Second
	countdown     = 3 * time.Second
	rematchWindow = 30 * time.Second
	// Guess My Answer: the guesser gets a second, longer window.
	guessWindow = 12 * time.Second
)

// gmaRoles is the per-round role assignment for Guess My Answer.
type gmaRoles struct {
	Answerer string `json:"answerer"`
	Guesser  string `json:"guesser"`
	Phase    string `json:"phase"`
}

// gmaFor decodes a round's Guess My Answer roles. It reports false for other
// game kinds.
func gmaFor(round store.Round) (gmaRoles, bool) {
	if round.Extra == nil {
		return gmaRoles{}, false
	}
	var roles gmaRoles
	if err := json.Unmarshal([]byte(*round.Extra), &roles); err != nil || roles.Answerer == "" || roles.Guesser == "" {
		return gmaRoles{}, false
	}
	if roles.Phase == "" {
		roles.Phase = "answer"
	}
	return roles, true
}

// revealRound closes a round and opens the reveal window.
func (e *Engine) revealRound(ctx context.Context, ses store.GameSession, round *store.Round, now time.Time) error {
	if err := e.Store.PutRoundState(ctx, round.ID, "reveal"); err != nil {
		return err
	}
	ses.State = "reveal_round"
	ses.AnswerBy = ptr(store.FmtTS(now.Add(revealWindow)))
	return e.Store.UpdateSession(ctx, ses)
}

// StartSession creates a session with its participants.
func (e *Engine) StartSession(ctx context.Context, kind, mode string, userIDs []string) (store.GameSession, error) {
	rounds := 8
	switch kind {
	case "twenty_questions", "guess_my_answer":
		rounds = 6
	}
	cfgJSON, _ := json.Marshal(map[string]any{"rounds": rounds, "countdown_ms": 3000, "reveal_ms": 4000})
	ses := &store.GameSession{
		Kind:         kind,
		Mode:         mode,
		State:        "pending",
		Config:       ptr(string(cfgJSON)),
		CurrentRound: 0,
	}
	if err := e.Store.InsertSession(ctx, ses); err != nil {
		return store.GameSession{}, err
	}
	now := store.NowTS()
	for i, uid := range userIDs {
		p := &store.Participant{SessionID: ses.ID, UserID: uid, Seat: i, LastPollAt: &now}
		if err := e.Store.InsertParticipant(ctx, p); err != nil {
			return store.GameSession{}, err
		}
	}
	return *ses, nil
}

// Join marks a participant present and starts the countdown when enough have joined.
func (e *Engine) Join(ctx context.Context, sessionID, userID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	ses, err := e.Store.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}
	p, err := e.Store.Participant(ctx, sessionID, userID)
	if err != nil {
		return err
	}
	if p == nil {
		return ErrForbidden
	}
	if err := e.Store.MarkJoined(ctx, sessionID, userID); err != nil {
		return err
	}
	needed := 2
	if ses.Mode == modePractice {
		needed = 1
	}
	joined, err := e.Store.CountJoined(ctx, sessionID)
	if err != nil {
		return err
	}
	if ses.State == "pending" && joined >= needed {
		ses.State = "countdown"
		started := store.FmtTS(time.Now().UTC().Add(countdown))
		ses.StartedAt = &started
		return e.Store.UpdateSession(ctx, ses)
	}
	return nil
}

// Heartbeat records a poll and advances the session.
func (e *Engine) Heartbeat(ctx context.Context, sessionID, userID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.Store.TouchPoll(ctx, sessionID, userID); err != nil {
		return err
	}
	return e.advanceLocked(ctx, sessionID)
}

// Answer records a player's answer and advances the session.
func (e *Engine) Answer(ctx context.Context, sessionID, userID string, payload json.RawMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.advanceLocked(ctx, sessionID); err != nil {
		return err
	}
	ses, err := e.Store.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}
	if ses.State != "in_round" {
		return ErrClosed
	}
	round, err := e.Store.RoundByIndex(ctx, sessionID, ses.CurrentRound)
	if err != nil {
		return err
	}
	if round == nil {
		return ErrNotFound
	}
	// Guess My Answer: only the role that owns the current phase may submit.
	if roles, ok := gmaFor(*round); ok {
		switch roles.Phase {
		case "answer":
			if userID != roles.Answerer {
				return ErrNotYourTurn
			}
		case "guess":
			if userID != roles.Guesser {
				return ErrNotYourTurn
			}
		}
	}
	body := string(payload)
	if body == "" {
		body = "null"
	}
	if err := e.Store.UpsertRoundAnswer(ctx, &store.RoundAnswer{RoundID: round.ID, UserID: userID, Payload: body}); err != nil {
		return err
	}
	if ses.Mode == modePractice {
		house := e.Cfg.HouseUserID
		if err := e.Store.UpsertRoundAnswer(ctx, &store.RoundAnswer{
			RoundID: round.ID, UserID: house, Payload: e.houseAnswer(ctx, ses.Kind, round.PromptID),
		}); err != nil {
			return err
		}
	}
	return e.advanceLocked(ctx, sessionID)
}

// Advance runs the state machine for a session.
func (e *Engine) Advance(ctx context.Context, sessionID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.advanceLocked(ctx, sessionID)
}

func terminal(state string) bool {
	return state == "completed" || state == "forfeit" || state == "cancelled"
}

func (e *Engine) advanceLocked(ctx context.Context, sessionID string) error {
	ses, err := e.Store.GetSession(ctx, sessionID)
	if err != nil {
		return ErrNotFound
	}
	if terminal(ses.State) {
		return nil
	}
	now := time.Now().UTC()

	if ses.Mode != modePractice {
		parts, err := e.Store.Participants(ctx, sessionID)
		if err != nil {
			return err
		}
		stale := false
		for _, p := range parts {
			if p.LastPollAt == nil {
				continue
			}
			if store.ParseTS(*p.LastPollAt).Before(now.Add(-pollStale)) {
				stale = true
			}
		}
		if stale && (ses.State == "countdown" || ses.State == "in_round" || ses.State == "reveal_round" || ses.State == "scoring") {
			return e.Store.PutSessionState(ctx, sessionID, "forfeit", ptr(store.FmtTS(now)))
		}
	}

	if ses.State == "pending" && ses.CreatedAt != nil && store.ParseTS(*ses.CreatedAt).Before(now.Add(-pendingTTL)) {
		return e.Store.PutSessionState(ctx, sessionID, "cancelled", ptr(store.FmtTS(now)))
	}

	if ses.State == "countdown" && ses.StartedAt != nil && !store.ParseTS(*ses.StartedAt).After(now) {
		if err := e.openRound(ctx, ses, 1); err != nil {
			return err
		}
		ses, err = e.Store.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
	}

	if ses.State == "in_round" {
		round, err := e.Store.RoundByIndex(ctx, sessionID, ses.CurrentRound)
		if err != nil {
			return err
		}
		if round == nil {
			return nil
		}
		answers, err := e.Store.AnswersForRound(ctx, round.ID)
		if err != nil {
			return err
		}
		timedOut := round.AnswerBy != nil && !store.ParseTS(*round.AnswerBy).After(now)

		// Guess My Answer runs in two phases: the answerer commits first, then
		// the guesser has a second (longer) window.
		if roles, ok := gmaFor(*round); ok {
			switch roles.Phase {
			case "answer":
				if answerFor(answers, roles.Answerer) != nil {
					roles.Phase = "guess"
					deadline := store.FmtTS(now.Add(guessWindow))
					extra, _ := json.Marshal(roles)
					if err := e.Store.PutRoundExtra(ctx, round.ID, string(extra), deadline); err != nil {
						return err
					}
					ses.AnswerBy = &deadline
					return e.Store.UpdateSession(ctx, ses)
				}
				if timedOut {
					// Nobody answered: skip the round without recording signals.
					return e.revealRound(ctx, ses, round, now)
				}
				return nil
			case "guess":
				if len(answers) >= 2 || timedOut {
					return e.revealRound(ctx, ses, round, now)
				}
				return nil
			}
		}

		if len(answers) >= 2 || timedOut {
			return e.revealRound(ctx, ses, round, now)
		}
	}

	if ses.State == "reveal_round" && ses.AnswerBy != nil && !store.ParseTS(*ses.AnswerBy).After(now) {
		total := sessionRounds(ses)
		if ses.CurrentRound >= total {
			return e.complete(ctx, ses)
		}
		return e.openRound(ctx, ses, ses.CurrentRound+1)
	}
	return nil
}

func sessionRounds(ses store.GameSession) int {
	if ses.Config == nil {
		return 8
	}
	var cfg struct {
		Rounds int `json:"rounds"`
	}
	if err := json.Unmarshal([]byte(*ses.Config), &cfg); err != nil || cfg.Rounds == 0 {
		return 8
	}
	return cfg.Rounds
}

func (e *Engine) openRound(ctx context.Context, ses store.GameSession, index int) error {
	prompt, err := e.Store.RandomPrompt(ctx, ses.Kind)
	if err != nil {
		return err
	}
	var extra *string
	if ses.Kind == "guess_my_answer" {
		parts, err := e.Store.Participants(ctx, ses.ID)
		if err != nil {
			return err
		}
		if len(parts) > 0 {
			answerer := parts[index%len(parts)]
			guesser := parts[0]
			for _, p := range parts {
				if p.UserID != answerer.UserID {
					guesser = p
					break
				}
			}
			b, _ := json.Marshal(gmaRoles{Answerer: answerer.UserID, Guesser: guesser.UserID, Phase: "answer"})
			extra = ptr(string(b))
		}
	}

	round, err := e.Store.RoundByIndex(ctx, ses.ID, index)
	if err != nil {
		return err
	}
	if round == nil {
		secs := 20 // twenty_questions
		switch ses.Kind {
		case "this_or_that":
			secs = 8
		case "guess_my_answer":
			// The answer phase is short; the guess phase gets guessWindow.
			secs = 8
		}
		answerBy := store.FmtTS(time.Now().UTC().Add(time.Duration(secs) * time.Second))
		var promptID *string
		if prompt != nil {
			promptID = &prompt.ID
		}
		round = &store.Round{
			SessionID:  ses.ID,
			RoundIndex: index,
			PromptID:   promptID,
			State:      "open",
			AnswerBy:   &answerBy,
			Extra:      extra,
		}
		if err := e.Store.InsertRound(ctx, round); err != nil {
			return err
		}
		// Practice: when the House holds the answer role, answer immediately so
		// the human is not stuck waiting on a bot that will not act.
		if ses.Kind == "guess_my_answer" && ses.Mode == modePractice {
			if roles, ok := gmaFor(*round); ok && roles.Answerer == e.Cfg.HouseUserID {
				payload := e.houseAnswer(ctx, ses.Kind, round.PromptID)
				if err := e.Store.UpsertRoundAnswer(ctx, &store.RoundAnswer{RoundID: round.ID, UserID: roles.Answerer, Payload: payload}); err != nil {
					return err
				}
				roles.Phase = "guess"
				deadline := store.FmtTS(time.Now().UTC().Add(guessWindow))
				encoded, _ := json.Marshal(roles)
				if err := e.Store.PutRoundExtra(ctx, round.ID, string(encoded), deadline); err != nil {
					return err
				}
				round.AnswerBy = &deadline
			}
		}
	}

	ses.State = "in_round"
	ses.CurrentRound = index
	ses.AnswerBy = round.AnswerBy
	return e.Store.UpdateSession(ctx, ses)
}

func (e *Engine) complete(ctx context.Context, ses store.GameSession) error {
	ses.State = "scoring"
	if err := e.Store.UpdateSession(ctx, ses); err != nil {
		return err
	}
	// Materialize behaviour stats before scoring so this session counts.
	if err := e.recordBehavior(ctx, ses); err != nil {
		return err
	}
	if _, err := e.ScoreSession(ctx, ses.ID); err != nil {
		return err
	}
	ses.State = "completed"
	ses.EndedAt = ptr(store.NowTS())
	if err := e.Store.UpdateSession(ctx, ses); err != nil {
		return err
	}

	parts, err := e.Store.Participants(ctx, ses.ID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.UserID)
	}
	for _, uid := range ids {
		if uid == e.Cfg.HouseUserID {
			continue
		}
		amount := 50
		kind := "session_complete"
		if ses.Mode == modePractice {
			amount = 5
			kind = "practice_complete"
		}
		if err := e.Store.InsertXpEvent(ctx, uid, kind, amount); err != nil {
			return err
		}
		u, err := e.Store.GetUser(ctx, uid)
		if err != nil {
			continue
		}
		xp := u.XP + amount
		if err := e.Store.SetUserXPLevel(ctx, uid, xp, LevelForXP(xp)); err != nil {
			return err
		}
	}

	if ses.Mode != modePractice && len(ids) == 2 {
		a, b := OrderedPair(ids[0], ids[1])
		pair, err := e.Store.PairFor(ctx, a, b)
		if err != nil {
			return err
		}
		if pair == nil {
			pair = &store.Pair{UserA: a, UserB: b, State: "open_play", AAction: "none", BAction: "none"}
		}
		cooling := false
		if pair.CooldownUntil != nil && store.ParseTS(*pair.CooldownUntil).After(time.Now().UTC()) {
			if pair.State == "closed" || pair.State == "unmatched" || pair.State == "blocked" {
				cooling = true
			}
		}
		if !cooling {
			pair.State = "open_play"
			pair.AAction = "none"
			pair.BAction = "none"
			pair.PendingExpiresAt = nil
			pair.OriginSessionID = &ses.ID
			if err := e.Store.UpsertPair(ctx, pair); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) houseAnswer(ctx context.Context, kind string, promptID *string) string {
	if kind == "this_or_that" {
		return `{"choice":"left"}`
	}
	optionID := "a"
	if promptID != nil {
		if p, err := e.Store.PromptByID(ctx, *promptID); err == nil && p != nil {
			var payload struct {
				Options []struct {
					ID string `json:"id"`
				} `json:"options"`
			}
			if err := json.Unmarshal([]byte(p.Payload), &payload); err == nil && len(payload.Options) > 0 {
				optionID = payload.Options[0].ID
			}
		}
	}
	b, _ := json.Marshal(map[string]string{"optionId": optionID})
	return string(b)
}

// RunTicker advances active sessions and expires invites on an interval.
func (e *Engine) RunTicker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = e.Store.ExpireStaleInvites(ctx)
			_ = e.Store.ExpireStaleMatches(ctx)
			ids, err := e.Store.ActiveSessionIDs(ctx)
			if err != nil {
				continue
			}
			for _, id := range ids {
				_ = e.Advance(ctx, id)
			}
		}
	}
}
