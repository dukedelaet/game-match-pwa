package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gamematch/internal/config"
	"gamematch/internal/db"
	"gamematch/internal/game"
	"gamematch/internal/store"
	"github.com/stretchr/testify/require"
)

// Directly counts how many times the wrapped handler runs for one key, with no
// database side effects to obscure the result.
func TestIdempotentRunsHandlerOnceForConcurrentSameKey(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(dir + "/t.db")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, db.Migrate(context.Background(), conn))

	cfg := config.Config{Addr: "x", DataDir: dir, Env: "test", HouseUserID: "h"}
	st := store.New(conn, cfg)
	srv := NewServer(st, game.NewEngine(st, cfg), game.NewMatcher(st, cfg), cfg)

	user := store.User{ID: "u1", Status: "active"}
	// A handler that blocks briefly so concurrent callers overlap.
	var runs int32
	handler := srv.idempotent("POST /v1/test", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&runs, 1)
		WriteJSON(w, http.StatusOK, map[string]string{"ran": "yes"})
	})

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/test", strings.NewReader(`{"a":1}`))
		req.Header.Set("Idempotency-Key", "race-key")
		req = req.WithContext(context.WithValue(req.Context(), userKey, user))
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); do() }()
	}
	wg.Wait()

	require.Equal(t, int32(1), atomic.LoadInt32(&runs),
		"the handler ran %d times for one idempotency key; it must run once", runs)
}
