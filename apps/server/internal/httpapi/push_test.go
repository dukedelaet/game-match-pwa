package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"gamematch/internal/push"
)

func TestPushSubscriptionRoundTrip(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")

	code, body := alex.json(http.MethodPost, "/v1/me/push", map[string]any{
		"endpoint": "https://push.example/abc",
		"keys":     map[string]any{"p256dh": "pkey", "auth": "akey"},
	})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, body["ok"])
	require.Equal(t, false, body["enabled"], "push is inert until VAPID keys are configured")

	subs, err := st.PushSubscriptionsFor(ctx, alexID)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.Equal(t, "pkey", subs[0].P256dh)

	// Refreshing the same endpoint updates rather than duplicates.
	code, _ = alex.json(http.MethodPost, "/v1/me/push", map[string]any{
		"endpoint": "https://push.example/abc",
		"keys":     map[string]any{"p256dh": "pkey2", "auth": "akey"},
	})
	require.Equal(t, http.StatusOK, code)
	subs, err = st.PushSubscriptionsFor(ctx, alexID)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.Equal(t, "pkey2", subs[0].P256dh)

	code, _ = alex.json(http.MethodDelete, "/v1/me/push", map[string]any{"endpoint": "https://push.example/abc"})
	require.Equal(t, http.StatusOK, code)
	subs, err = st.PushSubscriptionsFor(ctx, alexID)
	require.NoError(t, err)
	require.Empty(t, subs)
}

func TestPushSubscriptionValidation(t *testing.T) {
	s, _ := newTestServer(t)
	alex := login(t, s, "+15551111111")

	code, _ := alex.json(http.MethodPost, "/v1/me/push", map[string]any{"endpoint": "https://push.example/x"})
	require.Equal(t, http.StatusUnprocessableEntity, code, "keys are required")

	code, _ = alex.json(http.MethodDelete, "/v1/me/push", map[string]any{})
	require.Equal(t, http.StatusUnprocessableEntity, code)
}

func TestVapidKeyEndpointIsEmptyWhenUnconfigured(t *testing.T) {
	s, _ := newTestServer(t)
	alex := login(t, s, "+15551111111")

	code, body := alex.json(http.MethodGet, "/v1/push/vapid-public-key", nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "", body["publicKey"])
	require.Equal(t, false, body["enabled"])
}

func TestNotifyPushIsInertWithoutKeys(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")

	code, _ := alex.json(http.MethodPost, "/v1/me/push", map[string]any{
		"endpoint": "https://push.example/abc",
		"keys":     map[string]any{"p256dh": "pkey", "auth": "akey"},
	})
	require.Equal(t, http.StatusOK, code)

	// No VAPID keys: fan-out must return immediately without touching the network.
	require.NotPanics(t, func() {
		s.notifyPush(context.Background(), alexID, push.Message{Title: "hi"})
	})
}

func TestPushSubscriptionRequiresAuth(t *testing.T) {
	s, _ := newTestServer(t)
	code, _ := newClient(t, s).json(http.MethodPost, "/v1/me/push", map[string]any{"endpoint": "x"})
	require.Equal(t, http.StatusUnauthorized, code)
}
