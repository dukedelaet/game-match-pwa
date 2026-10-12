package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"gamematch/internal/game"
)

// writeSessionError maps an engine failure to a response. A user who is not in
// the session gets 403: the session IDs that reach clients are not secret, so
// they are not access control.
func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, game.ErrForbidden):
		WriteError(w, http.StatusForbidden, "forbidden", "Not in session")
	case errors.Is(err, game.ErrBadAnswer):
		WriteError(w, http.StatusUnprocessableEntity, "bad_answer", "That answer is not complete")
	case errors.Is(err, game.ErrNotYourTurn):
		WriteError(w, http.StatusForbidden, "not_your_turn", "It is not your turn")
	case errors.Is(err, game.ErrClosed):
		WriteError(w, http.StatusConflict, "closed", "Not accepting answers")
	default:
		notFound(w)
	}
}

func (s *Server) sessionJoin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	id := idParam(r)
	if err := s.Engine.Join(ctx, id, u.ID); err != nil {
		if errors.Is(err, game.ErrForbidden) {
			WriteError(w, http.StatusForbidden, "forbidden", "Not in session")
			return
		}
		notFound(w)
		return
	}
	view, err := s.Engine.View(ctx, id, u.ID)
	if err != nil {
		notFound(w)
		return
	}
	WriteJSON(w, http.StatusOK, view)
}

func (s *Server) sessionShow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	if !s.allow(w, r, "poll:"+u.ID, limitSessionPollPerUser, windowSessionPoll) {
		return
	}
	id := idParam(r)
	if err := s.Engine.Heartbeat(ctx, id, u.ID); err != nil {
		writeSessionError(w, err)
		return
	}
	view, err := s.Engine.View(ctx, id, u.ID)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, view)
}

func (s *Server) sessionAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	id := idParam(r)
	var body struct {
		Payload json.RawMessage `json:"payload"`
	}
	_ = decodeJSON(r, &body)
	payload := body.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("[]")
	}
	if err := s.Engine.Heartbeat(ctx, id, u.ID); err != nil {
		writeSessionError(w, err)
		return
	}
	if err := s.Engine.Answer(ctx, id, u.ID, payload); err != nil {
		writeSessionError(w, err)
		return
	}
	view, err := s.Engine.View(ctx, id, u.ID)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, view)
}

func (s *Server) sessionLeave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	id := idParam(r)
	// Leaving forfeits the game for both players, so only a player may do it.
	if err := s.Engine.Leave(ctx, id, u.ID); err != nil {
		writeSessionError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) rematch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	id := idParam(r)
	ses, err := s.Store.GetSession(ctx, id)
	if err != nil {
		notFound(w)
		return
	}
	if ses.Mode == "practice" {
		notFound(w)
		return
	}
	parts, err := s.Store.Participants(ctx, id)
	if err != nil {
		notFound(w)
		return
	}
	var other string
	found := false
	for _, p := range parts {
		if p.UserID == u.ID {
			found = true
		} else {
			other = p.UserID
		}
	}
	if !found || other == "" {
		notFound(w)
		return
	}
	s.createInvite(w, r, other, ses.Kind)
}

func trimSpace(s string) string { return strings.TrimSpace(s) }
