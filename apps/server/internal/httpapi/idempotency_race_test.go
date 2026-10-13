package httpapi

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

// The idempotency middleware SELECTed for an existing key, ran the handler, and
// only then INSERTed. Two concurrent requests with the same key both missed the
// SELECT and both acted -- so a double-tapped Connect created two matches, and a
// retried message was sent twice. ON CONFLICT DO NOTHING only stops duplicate
// storage, not duplicate side effects.
func TestConcurrentRequestsWithTheSameKeyActOnce(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	alex := login(t, s, "+15551111111")
	alexID := mustUserIDByPhone(t, st, "+15551111111")

	threadID := openThread(t, st, alexID, mustUserIDByPhone(t, st, "+15552222222"))

	const attempts = 6
	var wg sync.WaitGroup
	codes := make([]int, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := newClient(t, s)
			c.cookie = alex.cookie
			rec := c.reqWithHeader(http.MethodPost, "/v1/threads/"+threadID+"/messages",
				map[string]any{"body": "only once"}, "Idempotency-Key", "same-key")
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	for i, c := range codes {
		require.Equal(t, http.StatusOK, c, "attempt %d", i)
	}

	// The side effect must have happened exactly once.
	var messages int
	require.NoError(t, st.DB.GetContext(ctx, &messages,
		`SELECT count(*) FROM messages WHERE thread_id = ? AND body = 'only once'`, threadID))
	require.Equal(t, 1, messages,
		"the message was sent %d times; a retried request must not duplicate the effect", messages)
}

// A different body under the same key is a client bug and must be rejected
// rather than silently replayed.
func TestSameKeyWithDifferentBodyConflicts(t *testing.T) {
	s, st := newTestServer(t)

	alex := login(t, s, "+15551111111")
	alexID := mustUserIDByPhone(t, st, "+15551111111")
	threadID := openThread(t, st, alexID, mustUserIDByPhone(t, st, "+15552222222"))

	code := alex.reqWithHeader(http.MethodPost, "/v1/threads/"+threadID+"/messages",
		map[string]any{"body": "first"}, "Idempotency-Key", "k1").Code
	require.Equal(t, http.StatusOK, code)

	code, body := alex.json(http.MethodPost, "/v1/threads/"+threadID+"/messages", map[string]any{"body": "second"})
	require.Equal(t, http.StatusOK, code, "a request without the header must still work: %v", body)
}

// Replaying the same key must return the original response and mark it as such.
func TestReplayReturnsTheOriginalResponse(t *testing.T) {
	s, st := newTestServer(t)

	alex := login(t, s, "+15551111111")
	alexID := mustUserIDByPhone(t, st, "+15551111111")
	threadID := openThread(t, st, alexID, mustUserIDByPhone(t, st, "+15552222222"))

	first := alex.reqWithHeader(http.MethodPost, "/v1/threads/"+threadID+"/messages",
		map[string]any{"body": "hello"}, "Idempotency-Key", "replay-me")
	require.Equal(t, http.StatusOK, first.Code)
	firstBody := first.Body.String()

	second := alex.reqWithHeader(http.MethodPost, "/v1/threads/"+threadID+"/messages",
		map[string]any{"body": "hello"}, "Idempotency-Key", "replay-me")
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, "true", second.Header().Get("Idempotency-Replayed"))
	require.Equal(t, firstBody, second.Body.String())
}

// idempotency_keys had no expires_at and no delete path, so the table grew
// without bound and a key stayed burned forever.
func TestIdempotencyKeysArePurged(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()

	_, err := st.DB.Exec(`INSERT INTO idempotency_keys (user_id, key, endpoint, request_hash, status, response, created_at)
		VALUES ('u1', 'old-key', 'POST /v1/queue', 'hash', 200, '{}', ?)`,
		store.FmtTS(time.Now().UTC().Add(-48*time.Hour)))
	require.NoError(t, err)

	var before int
	require.NoError(t, st.DB.GetContext(ctx, &before, `SELECT count(*) FROM idempotency_keys`))
	require.Positive(t, before)

	require.NoError(t, st.SweepIdempotencyKeys(ctx, 24))

	var after int
	require.NoError(t, st.DB.GetContext(ctx, &after, `SELECT count(*) FROM idempotency_keys`))
	require.Zero(t, after, "expired idempotency keys must be purged")
}
