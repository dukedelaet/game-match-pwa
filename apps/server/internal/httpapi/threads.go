package httpapi

import (
	"net/http"

	"gamematch/internal/push"
	"gamematch/internal/store"
)

// threads lists the caller's conversations with their last message and the
// other person.
//
// This was 4 + 5N statements -- a pass per match for the block check, the
// thread, the last message and the other user's card -- with no LIMIT.
func (s *Server) threads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)

	matches, err := s.Store.MutualMatchesForLimited(ctx, u.ID, listLimit(r))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	items := []ThreadItem{}
	if len(matches) == 0 {
		WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}

	otherIDs := make([]string, 0, len(matches))
	matchIDs := make([]string, 0, len(matches))
	for _, m := range matches {
		other := m.UserA
		if u.ID == m.UserA {
			other = m.UserB
		}
		otherIDs = append(otherIDs, other)
		matchIDs = append(matchIDs, m.ID)
	}

	blocked, err := s.visibleTo(ctx, u.ID, otherIDs)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	threads, err := s.Store.ThreadsForMatches(ctx, matchIDs)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	threadIDs := make([]string, 0, len(matches))
	for _, th := range threads {
		if th != nil {
			threadIDs = append(threadIDs, th.ID)
		}
	}
	lasts, err := s.Store.LastMessagesForThreads(ctx, threadIDs)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	cards, err := s.cardsFor(ctx, otherIDs)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}

	for i, m := range matches {
		if blocked[otherIDs[i]] {
			continue
		}
		item := ThreadItem{MatchID: m.ID}
		if th := threads[m.ID]; th != nil {
			item.ThreadID = &th.ID
			item.State = &th.State
			if last := lasts[th.ID]; last != nil {
				item.Last = &LastMessage{Body: last.Body, Kind: last.Kind, At: last.CreatedAt}
			}
		}
		item.Other = cards[otherIDs[i]]
		items = append(items, item)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	thread, err := s.Store.ThreadByID(ctx, idParam(r))
	if err != nil {
		notFound(w)
		return
	}
	m, err := s.Store.MatchByID(ctx, thread.MatchID)
	if err != nil {
		notFound(w)
		return
	}
	if m.UserA != u.ID && m.UserB != u.ID {
		notFound(w)
		return
	}
	blocked, err := s.Store.BlockedEitherWay(ctx, m.UserA, m.UserB)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if blocked {
		notFound(w)
		return
	}
	msgs, err := s.Store.MessagesForThread(ctx, thread.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	items := make([]MessageItem, 0, len(msgs))
	for _, msg := range msgs {
		items = append(items, MessageItem{
			ID:        msg.ID,
			SenderID:  msg.SenderID,
			Kind:      msg.Kind,
			Body:      msg.Body,
			Meta:      jsonOrNull(msg.Meta),
			CreatedAt: msg.CreatedAt,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"state": thread.State, "items": items})
}

var allowedIcebreaker = []string{"I'll pick", "You pick", "Let's compete", "Play again"}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	thread, err := s.Store.ThreadByID(ctx, idParam(r))
	if err != nil {
		notFound(w)
		return
	}
	m, err := s.Store.MatchByID(ctx, thread.MatchID)
	if err != nil {
		notFound(w)
		return
	}
	if m.UserA != u.ID && m.UserB != u.ID {
		notFound(w)
		return
	}
	blocked, err := s.Store.BlockedEitherWay(ctx, m.UserA, m.UserB)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if blocked {
		notFound(w)
		return
	}
	if thread.State != "open" {
		WriteError(w, http.StatusForbidden, "locked", "Chat is locked")
		return
	}
	var body struct {
		Body string `json:"body"`
		Kind string `json:"kind"`
	}
	_ = decodeJSON(r, &body)
	text := trimSpace(body.Body)
	if text == "" {
		WriteError(w, http.StatusUnprocessableEntity, "empty", "Type something")
		return
	}
	if body.Kind == "icebreaker" && !containsString(allowedIcebreaker, text) {
		body.Kind = "text"
	}
	first, err := s.Store.CountTextMessages(ctx, thread.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if !s.allow(w, r, "messages:"+thread.ID, limitMessagesPerThread, windowMessages) {
		return
	}
	sender := u.ID
	msg := &store.Message{ThreadID: thread.ID, SenderID: &sender, Kind: "text", Body: text}
	if err := s.Store.InsertMessage(ctx, msg); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if first == 0 {
		if err := s.Store.InsertXpEvent(ctx, u.ID, "first_message", 10); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
		if err := s.Store.AddXP(ctx, u.ID, 10); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
	}
	if err := s.Store.ClearMatchExpiry(ctx, m.ID); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	other := m.UserA
	if u.ID == m.UserA {
		other = m.UserB
	}
	s.notifyPush(ctx, other, push.Message{
		Title: "New message",
		Body:  text,
		URL:   "/chat/" + thread.ID,
		Tag:   "thread-" + thread.ID,
	})
	WriteJSON(w, http.StatusOK, map[string]string{"id": msg.ID})
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
