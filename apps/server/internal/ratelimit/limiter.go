// Package ratelimit implements fixed-window request throttling backed by a
// persisted SQLite counter table, so windows survive a process restart.
package ratelimit

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
)

// Limiter counts attempts per bucket in fixed windows.
type Limiter struct {
	DB *sqlx.DB
}

// New builds a limiter over an open database.
func New(db *sqlx.DB) *Limiter { return &Limiter{DB: db} }

// Allow records an attempt against bucket and reports whether it is within
// limit for the current window. When it is not, retryAfter is the time until
// the window resets. Counters only advance while under the limit, so a caller
// that is already blocked does not extend its own window.
func (l *Limiter) Allow(ctx context.Context, bucket string, limit int, window time.Duration) (allowed bool, retryAfter time.Duration, err error) {
	now := time.Now().UTC().UnixMilli()
	windowMs := window.Milliseconds()
	cutoff := now - windowMs

	var count, windowStart int64
	err = l.DB.QueryRowxContext(ctx, `
		INSERT INTO rate_limits (bucket, count, window_start) VALUES (?, 1, ?)
		ON CONFLICT(bucket) DO UPDATE SET
			count = CASE WHEN rate_limits.window_start <= ? THEN 1 ELSE rate_limits.count + 1 END,
			window_start = CASE WHEN rate_limits.window_start <= ? THEN ? ELSE rate_limits.window_start END
		RETURNING count, window_start`,
		bucket, now, cutoff, cutoff, now,
	).Scan(&count, &windowStart)
	if err != nil {
		return false, 0, err
	}
	if count <= int64(limit) {
		return true, 0, nil
	}
	remaining := time.Duration(windowStart+windowMs-now) * time.Millisecond
	if remaining < 0 {
		remaining = 0
	}
	return false, remaining, nil
}

// Bump records an attempt unconditionally, for counters that never block (such
// as OTP failure counts feeding a later challenge).
func (l *Limiter) Bump(ctx context.Context, bucket string, window time.Duration) {
	_, _, _ = l.Allow(ctx, bucket, 1<<30, window)
}

// Count reports the current attempt count for a bucket without incrementing it.
func (l *Limiter) Count(ctx context.Context, bucket string, window time.Duration) (int, error) {
	var row struct {
		Count       int64 `db:"count"`
		WindowStart int64 `db:"window_start"`
	}
	err := l.DB.GetContext(ctx, &row, `SELECT count, window_start FROM rate_limits WHERE bucket = ?`, bucket)
	if err != nil {
		return 0, nil
	}
	if row.WindowStart <= time.Now().UTC().Add(-window).UnixMilli() {
		return 0, nil
	}
	return int(row.Count), nil
}

// Reset clears a bucket. Used when a challenge is passed.
func (l *Limiter) Reset(ctx context.Context, bucket string) error {
	_, err := l.DB.ExecContext(ctx, `DELETE FROM rate_limits WHERE bucket = ?`, bucket)
	return err
}
