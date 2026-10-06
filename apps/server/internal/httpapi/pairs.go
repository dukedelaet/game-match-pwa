package httpapi

import (
	"context"
	"net/http"
	"time"

	"gamematch/internal/push"
	"gamematch/internal/store"
)

func cooldownActive(p store.Pair) bool {
	return p.CooldownUntil != nil && store.ParseTS(*p.CooldownUntil).After(time.Now().UTC())
}

func inviteEligibleState(state string) bool {
	switch state {
	case "open_play", "mutual", "expired", "unmatched":
		return true
	}
	return false
}

// pairViewFor builds a PairItem for one viewer, or nil when the row is hidden.
func (s *Server) pairViewFor(ctx context.Context, p store.Pair, viewerID string) (*PairItem, error) {
	blocked, err := s.Store.BlockedEitherWay(ctx, p.UserA, p.UserB)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, nil
	}
	your, their := p.AAction, p.BAction
	if viewerID == p.UserB {
		your, their = p.BAction, p.AAction
	}
	if p.State == "closed" && your != "pass" {
		return nil, nil
	}
	otherID := p.UserA
	if viewerID == p.UserA {
		otherID = p.UserB
	}
	card, err := s.card(ctx, otherID)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, nil
	}
	return &PairItem{
		ID:             p.ID,
		State:          p.State,
		YourAction:     your,
		TheyConnected:  their == "connect",
		InviteEligible: inviteEligibleState(p.State) && !cooldownActive(p),
		Other:          card,
	}, nil
}

func (s *Server) pairs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	rows, err := s.Store.PairsForUser(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	items := []PairItem{}
	for _, p := range rows {
		item, err := s.pairViewFor(ctx, p, u.ID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
		if item != nil {
			items = append(items, *item)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) pairShow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	p, err := s.Store.PairByID(ctx, idParam(r))
	if err != nil {
		notFound(w)
		return
	}
	if p.UserA != u.ID && p.UserB != u.ID {
		notFound(w)
		return
	}
	item, err := s.pairViewFor(ctx, p, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if item == nil {
		notFound(w)
		return
	}
	WriteJSON(w, http.StatusOK, item)
}

func (s *Server) pairAct(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	var body struct {
		Action string `json:"action"`
	}
	_ = decodeJSON(r, &body)
	if body.Action != "connect" && body.Action != "pass" {
		WriteError(w, http.StatusUnprocessableEntity, "bad", "connect or pass")
		return
	}
	p, err := s.Store.PairByID(ctx, idParam(r))
	if err != nil {
		notFound(w)
		return
	}
	if p.UserA != u.ID && p.UserB != u.ID {
		notFound(w)
		return
	}
	blocked, err := s.Store.BlockedEitherWay(ctx, p.UserA, p.UserB)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if blocked {
		notFound(w)
		return
	}
	if p.State != "open_play" && p.State != "pending" {
		WriteError(w, http.StatusConflict, "closed", "That window closed")
		return
	}
	if u.ID == p.UserA {
		p.AAction = body.Action
	} else {
		p.BAction = body.Action
	}
	switch {
	case p.AAction == "pass" || p.BAction == "pass":
		p.State = "closed"
		cd := store.FmtTS(time.Now().UTC().Add(14 * 24 * time.Hour))
		p.CooldownUntil = &cd
	case p.AAction == "connect" && p.BAction == "connect":
		if err := s.mutual(ctx, &p); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
	default:
		p.State = "pending"
		pe := store.FmtTS(time.Now().UTC().Add(72 * time.Hour))
		p.PendingExpiresAt = &pe
	}
	if err := s.Store.UpsertPair(ctx, &p); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	s.pairShow(w, r)
}

// mutual transitions a pair into a match, opening (or reopening) its thread.
func (s *Server) mutual(ctx context.Context, p *store.Pair) error {
	p.State = "mutual"
	now := time.Now().UTC()
	expires := store.FmtTS(now.Add(7 * 24 * time.Hour))
	matchedAt := store.FmtTS(now)

	match, err := s.Store.MatchFor(ctx, p.UserA, p.UserB)
	if err != nil {
		return err
	}
	if match == nil {
		match = &store.Match{
			UserA:           p.UserA,
			UserB:           p.UserB,
			State:           "mutual",
			OriginSessionID: p.OriginSessionID,
			MatchedAt:       &matchedAt,
			ExpiresAt:       &expires,
		}
	} else {
		match.State = "mutual"
		match.OriginSessionID = p.OriginSessionID
		match.MatchedAt = &matchedAt
		match.ExpiresAt = &expires
		match.UnmatchedBy = nil
	}
	if err := s.Store.UpsertMatch(ctx, match); err != nil {
		return err
	}

	thread, err := s.Store.ThreadForMatch(ctx, match.ID)
	if err != nil {
		return err
	}
	if thread == nil {
		thread = &store.Thread{MatchID: match.ID, State: "open"}
		if err := s.Store.InsertThread(ctx, thread); err != nil {
			return err
		}
	} else {
		if err := s.Store.PutThreadState(ctx, thread.ID, "open"); err != nil {
			return err
		}
		if err := s.Store.InsertMessage(ctx, &store.Message{ThreadID: thread.ID, Kind: "system", Body: "You matched again"}); err != nil {
			return err
		}
	}
	meta := `{"actions":["me","them","compete","play_again"]}`
	if err := s.Store.InsertMessage(ctx, &store.Message{
		ThreadID: thread.ID,
		Kind:     "icebreaker",
		Body:     "You just played together — who picks the next move?",
		Meta:     &meta,
	}); err != nil {
		return err
	}
	for _, uid := range []string{p.UserA, p.UserB} {
		if err := s.Store.InsertXpEvent(ctx, uid, "mutual", 20); err != nil {
			return err
		}
		if err := s.Store.AddXP(ctx, uid, 20); err != nil {
			return err
		}
		s.notifyPush(ctx, uid, push.Message{
			Title: "You matched",
			Body:  "Say hi before the spark fades.",
			URL:   "/chat",
			Tag:   "match",
		})
	}
	return nil
}

func (s *Server) unmatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	p, err := s.Store.PairByID(ctx, idParam(r))
	if err != nil {
		notFound(w)
		return
	}
	if p.UserA != u.ID && p.UserB != u.ID {
		notFound(w)
		return
	}
	p.State = "unmatched"
	cd := store.FmtTS(time.Now().UTC().Add(14 * 24 * time.Hour))
	p.CooldownUntil = &cd
	if err := s.Store.UpsertPair(ctx, &p); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	match, err := s.Store.MatchFor(ctx, p.UserA, p.UserB)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if match != nil {
		uid := u.ID
		if err := s.Store.PutMatchState(ctx, match.ID, "unmatched", &uid); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
		if err := s.Store.PutThreadStateByMatch(ctx, match.ID, "locked"); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
