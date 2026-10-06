package game

import (
	"context"
	"encoding/json"

	"gamematch/internal/store"
)

// Deadline is the server-authoritative round timing.
type Deadline struct {
	AnswerBy  *string `json:"answerBy"`
	ServerNow string  `json:"serverNow"`
}

// RoundView is the in-round payload.
type RoundView struct {
	Index             int             `json:"index"`
	Total             int             `json:"total"`
	Kind              string          `json:"kind"`
	Prompt            json.RawMessage `json:"prompt"`
	YourRole          *string         `json:"yourRole"`
	Deadline          Deadline        `json:"deadline"`
	YouSubmitted      bool            `json:"youSubmitted"`
	OpponentSubmitted bool            `json:"opponentSubmitted"`
}

// RevealPayload wraps a player's submitted payload.
type RevealPayload struct {
	Payload json.RawMessage `json:"payload"`
}

// Reveal is the post-round comparison.
type Reveal struct {
	Index    int           `json:"index"`
	Kind     string        `json:"kind"`
	You      RevealPayload `json:"you"`
	Opponent RevealPayload `json:"opponent"`
	Same     bool          `json:"same"`
}

// SnapshotView is the compatibility result.
type SnapshotView struct {
	Score      float64         `json:"score"`
	Percent    *int            `json:"percent"`
	Reasons    []string        `json:"reasons"`
	Components json.RawMessage `json:"components"`
}

// PairView is the pair summary shown on completion.
type PairView struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	YourAction    string `json:"yourAction"`
	TheyConnected bool   `json:"theyConnected"`
}

// Completed is the terminal session payload.
type Completed struct {
	SessionID        string        `json:"sessionId"`
	PairID           *string       `json:"pairId"`
	ConnectEligible  bool          `json:"connectEligible"`
	RematchUntil     *string       `json:"rematchUntil"`
	Snapshot         *SnapshotView `json:"snapshot"`
	Pair             *PairView     `json:"pair"`
	PracticeOpponent *string       `json:"practiceOpponent"`
}

// OpponentView is the other player.
type OpponentView struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
	Age  *int    `json:"age"`
}

// SessionView is the full session state returned to a player.
type SessionView struct {
	SessionID   string        `json:"sessionId"`
	State       string        `json:"state"`
	Kind        string        `json:"kind"`
	Mode        string        `json:"mode"`
	YouSeat     int           `json:"youSeat"`
	ResumeToken string        `json:"resumeToken"`
	Opponent    *OpponentView `json:"opponent"`
	Round       *RoundView    `json:"round"`
	Reveal      *Reveal       `json:"reveal"`
	Completed   *Completed    `json:"completed"`
	ServerNow   string        `json:"serverNow"`
}

// View advances the session and returns its current state for a player.
func (e *Engine) View(ctx context.Context, sessionID, userID string) (SessionView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.advanceLocked(ctx, sessionID); err != nil {
		return SessionView{}, err
	}
	return e.buildView(ctx, sessionID, userID)
}

// ViewNoAdvance builds the view without advancing (used after a locked call).
func (e *Engine) buildView(ctx context.Context, sessionID, userID string) (SessionView, error) {
	ses, err := e.Store.GetSession(ctx, sessionID)
	if err != nil {
		return SessionView{}, ErrNotFound
	}
	parts, err := e.Store.Participants(ctx, sessionID)
	if err != nil {
		return SessionView{}, err
	}

	seat := 0
	var oppID string
	for _, p := range parts {
		if p.UserID == userID {
			seat = p.Seat
		} else if oppID == "" {
			oppID = p.UserID
		}
	}
	isHouse := oppID != "" && oppID == e.Cfg.HouseUserID
	now := store.NowTS()

	round, err := e.Store.RoundByIndex(ctx, sessionID, ses.CurrentRound)
	if err != nil {
		return SessionView{}, err
	}

	var roundView *RoundView
	var reveal *Reveal
	if round != nil && (ses.State == "in_round" || ses.State == "reveal_round") {
		var promptRaw json.RawMessage = json.RawMessage("null")
		if round.PromptID != nil {
			if p, err := e.Store.PromptByID(ctx, *round.PromptID); err == nil && p != nil {
				promptRaw = json.RawMessage(p.Payload)
			}
		}
		answers, err := e.Store.AnswersForRound(ctx, round.ID)
		if err != nil {
			return SessionView{}, err
		}
		you := answerFor(answers, userID)
		them := answerFor(answers, oppID)

		var yourRole *string
		if ses.Kind == "guess_my_answer" && round.Extra != nil {
			var extra struct {
				Answerer string `json:"answerer"`
				Guesser  string `json:"guesser"`
			}
			if err := json.Unmarshal([]byte(*round.Extra), &extra); err == nil {
				role := "guesser"
				if extra.Answerer == userID {
					role = "answerer"
				}
				yourRole = &role
			}
		}

		roundView = &RoundView{
			Index:             round.RoundIndex,
			Total:             sessionRounds(ses),
			Kind:              ses.Kind,
			Prompt:            promptRaw,
			YourRole:          yourRole,
			Deadline:          Deadline{AnswerBy: round.AnswerBy, ServerNow: now},
			YouSubmitted:      you != nil,
			OpponentSubmitted: them != nil,
		}

		if ses.State == "reveal_round" {
			same := you != nil && them != nil && jsonEqual(you.Payload, them.Payload)
			if ses.Kind == "guess_my_answer" && round.Extra != nil {
				var extra struct {
					Answerer string `json:"answerer"`
					Guesser  string `json:"guesser"`
				}
				if err := json.Unmarshal([]byte(*round.Extra), &extra); err == nil {
					ans := answerFor(answers, extra.Answerer)
					guess := answerFor(answers, extra.Guesser)
					same = ans != nil && guess != nil && jsonEqual(ans.Payload, guess.Payload)
				}
			}
			reveal = &Reveal{
				Index:    round.RoundIndex,
				Kind:     ses.Kind,
				You:      RevealPayload{Payload: payloadOrNull(you)},
				Opponent: RevealPayload{Payload: payloadOrNull(them)},
				Same:     same,
			}
		}
	}

	var completed *Completed
	if ses.State == "completed" {
		snap, err := e.Store.SnapshotForSession(ctx, sessionID)
		if err != nil {
			return SessionView{}, err
		}
		var pairID *string
		var pairView *PairView
		if oppID != "" && !isHouse {
			a, b := OrderedPair(userID, oppID)
			pr, err := e.Store.PairFor(ctx, a, b)
			if err != nil {
				return SessionView{}, err
			}
			if pr != nil {
				pairID = &pr.ID
				your, their := pr.AAction, pr.BAction
				if userID == pr.UserB {
					your, their = pr.BAction, pr.AAction
				}
				pairView = &PairView{ID: pr.ID, State: pr.State, YourAction: your, TheyConnected: their == "connect"}
			}
		}
		var rematchUntil *string
		if ses.EndedAt != nil {
			t := store.ParseTS(*ses.EndedAt).Add(rematchWindow)
			rematchUntil = ptr(store.FmtTS(t))
		}
		var practiceOpponent *string
		if isHouse {
			practiceOpponent = ptr("House")
		}
		completed = &Completed{
			SessionID:        sessionID,
			PairID:           pairID,
			ConnectEligible:  ses.Mode != modePractice && ses.State == "completed",
			RematchUntil:     rematchUntil,
			Snapshot:         snapshotView(snap),
			Pair:             pairView,
			PracticeOpponent: practiceOpponent,
		}
	}

	var opp *OpponentView
	if oppID != "" && !isHouse {
		if ou, err := e.Store.GetUser(ctx, oppID); err == nil {
			var age *int
			if prof, _ := e.Store.ProfileFor(ctx, oppID); prof != nil {
				age = prof.Age
			}
			name := ou.Name
			opp = &OpponentView{ID: ou.ID, Name: name, Age: age}
		}
	}
	if isHouse {
		opp = &OpponentView{ID: "house", Name: ptr("House"), Age: nil}
	}

	return SessionView{
		SessionID:   ses.ID,
		State:       ses.State,
		Kind:        ses.Kind,
		Mode:        ses.Mode,
		YouSeat:     seat,
		ResumeToken: ses.ID,
		Opponent:    opp,
		Round:       roundView,
		Reveal:      reveal,
		Completed:   completed,
		ServerNow:   now,
	}, nil
}

func answerFor(answers []store.RoundAnswer, userID string) *store.RoundAnswer {
	if userID == "" {
		return nil
	}
	for i := range answers {
		if answers[i].UserID == userID {
			return &answers[i]
		}
	}
	return nil
}

func payloadOrNull(a *store.RoundAnswer) json.RawMessage {
	if a == nil || a.Payload == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(a.Payload)
}

func snapshotView(snap *store.Snapshot) *SnapshotView {
	if snap == nil {
		return nil
	}
	var components map[string]any
	if snap.Components != nil {
		_ = json.Unmarshal([]byte(*snap.Components), &components)
	}
	var percent *int
	if components != nil {
		if v, ok := components["percent"]; ok {
			if f, ok := v.(float64); ok {
				p := int(f)
				percent = &p
			}
		}
	}
	reasons := []string{}
	if snap.Reasons != nil {
		_ = json.Unmarshal([]byte(*snap.Reasons), &reasons)
	}
	compRaw := json.RawMessage("null")
	if snap.Components != nil {
		compRaw = json.RawMessage(*snap.Components)
	}
	return &SnapshotView{Score: snap.Score, Percent: percent, Reasons: reasons, Components: compRaw}
}
