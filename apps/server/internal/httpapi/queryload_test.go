package httpapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// queueStatus was the only queue route with no rate limit, and the endpoint the
// PWA polls. Each poll ran a matching pass that issued roughly 15 queries per
// candidate for up to 20 candidates.
func TestQueueStatusIsRateLimited(t *testing.T) {
	s, _ := newTestServer(t)
	s.DisableRateLimits = false

	alex := login(t, s, "+15551111111")
	code, body := alex.json(http.MethodPost, "/v1/queue", map[string]string{"gameKind": "this_or_that"})
	require.Equal(t, http.StatusOK, code, "queue join: %v", body)

	var limited int
	for i := 0; i < 40; i++ {
		rec := alex.req(http.MethodGet, "/v1/queue/status", nil)
		if rec.Code == http.StatusTooManyRequests {
			limited++
		}
		require.NotEqual(t, http.StatusInternalServerError, rec.Code, "poll should never 500")
	}
	require.Positive(t, limited, "queue/status must throttle a client that polls without pause")
}

// A single client must not be able to hold the service busy by polling the
// matching endpoint as fast as it likes.
func TestQueueStatusPollsAreBoundedPerUser(t *testing.T) {
	s, _ := newTestServer(t)
	s.DisableRateLimits = false
	alex := login(t, s, "+15551111111")
	_, _ = alex.json(http.MethodPost, "/v1/queue", map[string]string{"gameKind": "this_or_that"})

	var allowed, limited int
	for i := 0; i < 120; i++ {
		rec := alex.req(http.MethodGet, "/v1/queue/status", nil)
		switch rec.Code {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			limited++
		}
	}

	require.Positive(t, limited, "unthrottled polling should be rejected")
	require.LessOrEqual(t, allowed, limitQueueStatusPerUser*2,
		"allowed %d polls, which is more than the configured budget", allowed)
}
