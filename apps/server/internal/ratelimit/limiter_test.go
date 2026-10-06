package ratelimit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"gamematch/internal/db"
)

func newLimiter(t *testing.T) (*Limiter, *sqlx.DB) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), conn))
	return New(conn), conn
}

func TestAllowEnforcesTheLimitWithinAWindow(t *testing.T) {
	l, _ := newLimiter(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		allowed, _, err := l.Allow(ctx, "queue:u1", 3, time.Minute)
		require.NoError(t, err)
		require.True(t, allowed, "attempt %d should be allowed", i+1)
	}

	allowed, retry, err := l.Allow(ctx, "queue:u1", 3, time.Minute)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Positive(t, retry, "a blocked caller gets a Retry-After hint")
}

func TestBlockedCallerDoesNotExtendItsOwnWindow(t *testing.T) {
	l, _ := newLimiter(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _, err := l.Allow(ctx, "b", 3, time.Minute)
		require.NoError(t, err)
	}
	_, first, err := l.Allow(ctx, "b", 3, time.Minute)
	require.NoError(t, err)
	_, second, err := l.Allow(ctx, "b", 3, time.Minute)
	require.NoError(t, err)

	require.LessOrEqual(t, second, first, "hammering must not push the reset further out")
}

func TestWindowRollsOver(t *testing.T) {
	l, conn := newLimiter(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _, err := l.Allow(ctx, "b", 3, time.Minute)
		require.NoError(t, err)
	}
	allowed, _, err := l.Allow(ctx, "b", 3, time.Minute)
	require.NoError(t, err)
	require.False(t, allowed)

	// Age the window past its duration.
	_, err = conn.Exec(`UPDATE rate_limits SET window_start = window_start - ? WHERE bucket = 'b'`, (2 * time.Minute).Milliseconds())
	require.NoError(t, err)

	allowed, _, err = l.Allow(ctx, "b", 3, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed, "a new window starts fresh")
}

func TestBucketsAreIndependent(t *testing.T) {
	l, _ := newLimiter(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _, _ = l.Allow(ctx, "queue:u1", 3, time.Minute)
	}
	allowed, _, err := l.Allow(ctx, "queue:u2", 3, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed, "one user's quota must not affect another's")
}

func TestBumpCountAndReset(t *testing.T) {
	l, _ := newLimiter(t)
	ctx := context.Background()

	l.Bump(ctx, "otp:fail:ip:1.2.3.4", time.Minute)
	l.Bump(ctx, "otp:fail:ip:1.2.3.4", time.Minute)

	n, err := l.Count(ctx, "otp:fail:ip:1.2.3.4", time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	require.NoError(t, l.Reset(ctx, "otp:fail:ip:1.2.3.4"))
	n, err = l.Count(ctx, "otp:fail:ip:1.2.3.4", time.Minute)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestCountIgnoresExpiredWindows(t *testing.T) {
	l, conn := newLimiter(t)
	ctx := context.Background()

	l.Bump(ctx, "old", time.Minute)
	_, err := conn.Exec(`UPDATE rate_limits SET window_start = window_start - ? WHERE bucket = 'old'`, (2 * time.Minute).Milliseconds())
	require.NoError(t, err)

	n, err := l.Count(ctx, "old", time.Minute)
	require.NoError(t, err)
	require.Zero(t, n)
}
