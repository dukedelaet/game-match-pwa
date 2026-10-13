package game

import (
	"bytes"
	"context"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The ticker ran on a bare goroutine with no recover and discarded every error.
// A panic inside Advance killed the whole process, taking every live game with
// it, and a transient SQLITE_BUSY silently disabled forfeit and expiry with no
// operator signal.
func TestTickerSurvivesAPanicAndKeepsRunning(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	// A session whose row is corrupt enough that Advance will panic: the round
	// points at a round_id that does not exist in a way the scorer dereferences.
	var sessions []string
	for i := 0; i < 2; i++ {
		ses, err := e.StartSession(ctx, "this_or_that", "queue_1v1", []string{alex, jordan})
		require.NoError(t, err)
		sessions = append(sessions, ses.ID)
	}
	// Corrupt one session's current_round so at least one tick has to work
	// through a path that can panic.
	_, err := st.DB.Exec(`UPDATE game_sessions SET current_round = 9999 WHERE id = ?`, sessions[0])
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.tickOnce(ctx)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the tick never returned; it is probably not recovering from a panic")
	}

	// The engine must still be usable afterwards, which it would not be if the
	// goroutine had died.
	require.NotNil(t, e)
	require.NoError(t, e.Advance(context.Background(), sessions[1]),
		"the engine must still work after a recovered panic")
}

// Every error the ticker encounters must reach the log rather than being
// assigned to _. Discarding them made a broken service look healthy.
func TestTickerLogsRatherThanDiscardsErrors(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()

	// Close the store's database so every call inside a tick fails.
	closed := st.DB
	require.NoError(t, closed.Close())
	t.Cleanup(func() { _ = st.DB.Ping() })

	logs := captureLogs(t)
	require.NotPanics(t, func() { e.tickOnce(ctx) })
	require.NotEmpty(t, logs.String(), "a failing tick must log why")
}

// The purge of expired limiter rows is what stops rate_limits growing forever.
func TestTickerSweepsExpiredRateLimits(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()

	_, err := st.DB.Exec(`INSERT INTO rate_limits (bucket, count, window_start) VALUES ('old', 1, ?)`,
		time.Now().UTC().Add(-48*time.Hour).UnixMilli())
	require.NoError(t, err)

	e.tickOnce(ctx)

	var n int
	require.NoError(t, st.DB.GetContext(ctx, &n,
		`SELECT count(*) FROM rate_limits WHERE bucket = 'old'`))
	require.Zero(t, n, "expired rate-limit rows must be swept by the ticker")
}

// captureLogs redirects the standard logger for the duration of a test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(log.LstdFlags) })
	return &buf
}
