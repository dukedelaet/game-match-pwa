package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"gamematch/internal/db"
)

// The bulk loader replaced a per-user query, so it must pick the same photo the
// single-user version would.
func TestFirstOKPhotosForMatchesFirstOKPhoto(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "t.db"))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, db.Migrate(context.Background(), conn))

	s := New(conn, testConfig(dir))
	ctx := context.Background()
	now := NowTS()

	// Two users, several photos each, including non-ok ones that must be
	// ignored, and ties on created_at.
	rows := []struct {
		id, user, state, created string
	}{
		{"p1", "u1", "ok", "2024-01-03T00:00:00Z"},
		{"p2", "u1", "ok", "2024-01-01T00:00:00Z"},
		{"p3", "u1", "hidden", "2020-01-01T00:00:00Z"},
		{"p4", "u2", "ok", "2024-02-02T00:00:00Z"},
		{"p5", "u2", "ok", "2024-02-01T00:00:00Z"},
		{"p6", "u3", "ok", "2024-03-03T00:00:00Z"},
	}
	for _, r := range rows {
		_, err := conn.ExecContext(ctx, `
			INSERT INTO photos (id, user_id, path, moderation_state, created_at, updated_at)
			VALUES (?,?,?,?,?,?)`, r.id, r.user, r.id+".jpg", r.state, r.created, now)
		require.NoError(t, err)
	}
	_, err = conn.ExecContext(ctx,
		`INSERT INTO users (id, name, status, onboarding_step, created_at, updated_at)
		 VALUES ('u1','A','active','done',?,?),('u2','B','active','done',?,?),('u3','C','active','done',?,?)`,
		now, now, now, now, now, now)
	require.NoError(t, err)

	ids := []string{"u1", "u2", "u3"}
	got, err := s.FirstOKPhotosFor(ctx, ids)
	require.NoError(t, err)

	for _, id := range ids {
		want, err := s.FirstOKPhoto(ctx, id)
		require.NoError(t, err)
		if want == nil {
			require.Nil(t, got[id], "user %s", id)
			continue
		}
		require.NotNil(t, got[id], "user %s", id)
		require.Equalf(t, want.ID, got[id].ID,
			"bulk loader picked a different photo for %s than the single-user query", id)
	}
}

// A user with no photos must read as absent or nil, never a zero Photo.
func TestFirstOKPhotosForHandlesMissingRows(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "t.db"))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, db.Migrate(context.Background(), conn))

	s := New(conn, testConfig(dir))
	got, err := s.FirstOKPhotosFor(context.Background(), []string{"nobody"})
	require.NoError(t, err)
	require.Nil(t, got["nobody"])

	empty, err := s.FirstOKPhotosFor(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

// ClampListLimit must never return a value that would produce a 500 or an
// unbounded result set.
func TestClampListLimit(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"", DefaultListLimit},
		{"abc", DefaultListLimit},
		{"-5", DefaultListLimit},
		{"0", DefaultListLimit},
		{"1", 1},
		{"25", 25},
		{"100000", MaxListLimit},
		{"9999999999999999999999", MaxListLimit}, // overflows int64
	}
	for _, tt := range tests {
		require.Equalf(t, tt.want, ClampListLimit(tt.raw), "ClampListLimit(%q)", tt.raw)
	}
}

// The bulk last-message loader replaced a per-thread query and must agree with
// it, including the ordering tie-break.
func TestLastMessagesForThreadsMatchesLastMessage(t *testing.T) {
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "t.db"))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, db.Migrate(context.Background(), conn))

	s := New(conn, testConfig(dir))
	ctx := context.Background()
	now := NowTS()

	_, err = conn.ExecContext(ctx, `
		INSERT INTO users (id, name, status, onboarding_step, created_at, updated_at)
		VALUES ('u1','A','active','done',?,?),('u2','B','active','done',?,?)`, now, now, now, now)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO users (id, name, status, onboarding_step, created_at, updated_at)
		VALUES ('u3','C','active','done',?,?)`, now, now)
	require.NoError(t, err)

	// matches is unique per (user_a, user_b), so each thread needs its own pair.
	for i, pair := range [][2]string{{"u1", "u2"}, {"u1", "u3"}} {
		id := fmt.Sprintf("m%d", i+1)
		_, err = conn.ExecContext(ctx, `
			INSERT INTO matches (id,user_a,user_b,state,matched_at,created_at,updated_at)
			VALUES (?,?,?,'mutual',?,?,?)`, id, pair[0], pair[1], now, now, now)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, `
			INSERT INTO chat_threads (id,match_id,state,created_at,updated_at)
			VALUES (?,?,'open',?,?)`, "t-"+id, id, now, now)
		require.NoError(t, err)
	}

	// m1's thread has several messages, including two sharing a created_at.
	msgs := []struct{ id, thread, body, at string }{
		{"msg1", "t-m1", "old", "2024-01-01T00:00:00Z"},
		{"msg2", "t-m1", "tie-a", "2024-02-01T00:00:00Z"},
		{"msg3", "t-m1", "tie-b", "2024-02-01T00:00:00Z"},
		{"msg4", "t-m1", "newest", "2024-03-01T00:00:00Z"},
		{"msg5", "t-m2", "only", "2024-01-15T00:00:00Z"},
	}
	for _, m := range msgs {
		_, err = conn.ExecContext(ctx, `
			INSERT INTO messages (id,thread_id,sender_id,kind,body,created_at,updated_at)
			VALUES (?,?,'u1','text',?,?,?)`, m.id, m.thread, m.body, m.at, m.at)
		require.NoError(t, err)
	}

	got, err := s.LastMessagesForThreads(ctx, []string{"t-m1", "t-m2"})
	require.NoError(t, err)

	for _, tid := range []string{"t-m1", "t-m2"} {
		want, err := s.LastMessage(ctx, tid)
		require.NoError(t, err)
		require.NotNilf(t, got[tid], "thread %s", tid)
		require.Equalf(t, want.ID, got[tid].ID,
			"bulk loader picked %s for %s but LastMessage picked %s", got[tid].ID, tid, want.ID)
	}

	// A thread with no messages must not appear.
	got, err = s.LastMessagesForThreads(ctx, []string{"t-nonexistent"})
	require.NoError(t, err)
	require.Empty(t, got)
}
