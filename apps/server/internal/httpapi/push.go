package httpapi

import (
	"context"
	"net/http"

	"gamematch/internal/push"
	"gamematch/internal/store"
)

// vapidPublicKey exposes the key the browser needs to create a subscription.
func (s *Server) vapidPublicKey(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"publicKey": s.Push.PublicKey(),
		"enabled":   s.Push.Enabled(),
	})
}

// subscribePush stores a browser push subscription for the caller.
func (s *Server) subscribePush(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)

	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	_ = decodeJSON(r, &body)
	if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		WriteError(w, http.StatusUnprocessableEntity, "bad", "A complete subscription is required")
		return
	}

	agent := r.UserAgent()
	subscription := store.PushSubscription{
		UserID:    u.ID,
		Endpoint:  body.Endpoint,
		P256dh:    body.Keys.P256dh,
		Auth:      body.Keys.Auth,
		UserAgent: &agent,
	}
	if err := s.Store.SavePushSubscription(ctx, subscription); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": s.Push.Enabled()})
}

// unsubscribePush removes one of the caller's endpoints.
func (s *Server) unsubscribePush(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)

	var body struct {
		Endpoint string `json:"endpoint"`
	}
	_ = decodeJSON(r, &body)
	if body.Endpoint == "" {
		WriteError(w, http.StatusUnprocessableEntity, "bad", "An endpoint is required")
		return
	}
	if err := s.Store.DeletePushSubscription(ctx, u.ID, body.Endpoint); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// notifyPush fans a notification out to a user's devices. It is a no-op when
// push is not configured and never blocks the request path.
func (s *Server) notifyPush(ctx context.Context, userID string, message push.Message) {
	if !s.Push.Enabled() {
		return
	}
	subscriptions, err := s.Store.PushSubscriptionsFor(ctx, userID)
	if err != nil || len(subscriptions) == 0 {
		return
	}
	out := make([]push.Subscription, 0, len(subscriptions))
	for _, sub := range subscriptions {
		out = append(out, push.Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth})
	}
	s.Push.Send(ctx, out, message)
}
