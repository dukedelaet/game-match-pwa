package httpapi

import (
	"net/http"
	"time"

	"gamematch/internal/game"
	"gamematch/internal/push"
	"gamematch/internal/store"
)

func (s *Server) inviteCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TargetUserID string `json:"targetUserId"`
		GameKind     string `json:"gameKind"`
	}
	_ = decodeJSON(r, &body)
	if !s.allow(w, r, "invites:"+currentUser(r).ID, limitInvitesPerUser, windowInvites) {
		return
	}
	kind := body.GameKind
	if kind == "" {
		kind = "this_or_that"
	}
	s.createInvite(w, r, body.TargetUserID, kind)
}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request, targetID, kind string) {
	ctx := r.Context()
	u := currentUser(r)
	if targetID == "" {
		notFound(w)
		return
	}
	blocked, err := s.Store.BlockedEitherWay(ctx, u.ID, targetID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if blocked {
		notFound(w)
		return
	}
	a, b := game.OrderedPair(u.ID, targetID)
	pair, err := s.Store.PairFor(ctx, a, b)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if pair == nil {
		WriteError(w, http.StatusConflict, "unknown", "Play together first")
		return
	}
	if pair.CooldownUntil != nil && store.ParseTS(*pair.CooldownUntil).After(time.Now().UTC()) {
		WriteError(w, http.StatusConflict, "cooldown", "Try again later")
		return
	}
	inv := &store.Invite{
		FromUserID: u.ID,
		ToUserID:   targetID,
		GameKind:   kind,
		State:      "pending",
		ExpiresAt:  store.FmtTS(time.Now().UTC().Add(2 * time.Minute)),
	}
	if err := s.Store.InsertInvite(ctx, inv); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	item, err := s.inviteItem(ctx, *inv, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	s.notifyPush(ctx, targetID, push.Message{
		Title: "Someone wants to play",
		Body:  "You have two minutes to accept.",
		URL:   "/home",
		Tag:   "invite",
	})
	WriteJSON(w, http.StatusOK, item)
}

func (s *Server) inviteAccept(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	inv, err := s.Store.GetInvite(ctx, idParam(r))
	if err != nil || inv.ToUserID != u.ID || inv.State != "pending" {
		notFound(w)
		return
	}
	if err := s.Store.PutInviteState(ctx, inv.ID, "accepted"); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	session, err := s.Engine.StartSession(ctx, inv.GameKind, "invite", []string{inv.FromUserID, inv.ToUserID})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"sessionId": session.ID, "gameKind": inv.GameKind})
}

func (s *Server) inviteDecline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	inv, err := s.Store.GetInvite(ctx, idParam(r))
	if err != nil || inv.ToUserID != u.ID {
		notFound(w)
		return
	}
	if err := s.Store.PutInviteState(ctx, inv.ID, "declined"); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
