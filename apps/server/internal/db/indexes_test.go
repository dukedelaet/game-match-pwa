package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func migrated(t *testing.T) *sqlx.DB {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, Migrate(context.Background(), c))
	return c
}

// plan runs EXPLAIN QUERY PLAN and flattens the detail column.
func plan(t *testing.T, c *sqlx.DB, query string, args ...any) string {
	t.Helper()
	rows, err := c.QueryxContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notused, &detail))
		out = append(out, detail)
	}
	require.NoError(t, rows.Err())
	return strings.Join(out, " | ")
}

// requireIndexUsed fails when the planner falls back to scanning the table.
// Scanning a covering index is fine and expected -- what matters is that the
// table itself is not read in full, so a bare "SCAN <table>" is the failure
// and "SCAN <table> USING INDEX ..." is the pass.
func requireIndexUsed(t *testing.T, c *sqlx.DB, label, query string, args ...any) {
	t.Helper()
	got := plan(t, c, query, args...)
	for _, seg := range strings.Split(got, "|") {
		seg = strings.TrimSpace(seg)
		if !strings.HasPrefix(seg, "SCAN ") {
			continue
		}
		if strings.Contains(seg, "USING INDEX") || strings.Contains(seg, "USING COVERING INDEX") {
			continue
		}
		t.Errorf("%s scans the table; plan was: %s", label, got)
	}
}

// These are the queries on the hot paths: the home feed, the match and thread
// lists, and the expiry sweep the ticker runs every five seconds. Three of
// them were full table scans, three times a minute per query, forever.
func TestHomeFeedUsesAnIndex(t *testing.T) {
	c := migrated(t)
	requireIndexUsed(t, c, "mutual matches for a user",
		`SELECT * FROM matches WHERE state='mutual' AND (user_a=? OR user_b=?) ORDER BY matched_at DESC`,
		"a", "b")
}

func TestMatchExpirySweepUsesAnIndex(t *testing.T) {
	c := migrated(t)
	requireIndexUsed(t, c, "expired mutual matches",
		`SELECT * FROM matches WHERE state='mutual' AND expires_at IS NOT NULL AND expires_at<=?`,
		"now")
}

func TestActiveSessionSweepUsesAnIndex(t *testing.T) {
	c := migrated(t)
	requireIndexUsed(t, c, "active sessions",
		`SELECT id FROM game_sessions WHERE state NOT IN ('completed','forfeit','cancelled')`)
}

func TestPendingInvitesForUserUsesAnIndex(t *testing.T) {
	c := migrated(t)
	requireIndexUsed(t, c, "pending invites",
		`SELECT * FROM invites WHERE to_user_id=? AND state='pending' AND expires_at>? ORDER BY created_at`,
		"user", "now")
}

func TestInviteExpirySweepUsesAnIndex(t *testing.T) {
	c := migrated(t)
	requireIndexUsed(t, c, "expired invites",
		`SELECT * FROM invites WHERE state='pending' AND expires_at<=?`, "now")
}

func TestPendingPairCountUsesAnIndex(t *testing.T) {
	c := migrated(t)
	requireIndexUsed(t, c, "pending pair count",
		`SELECT count(*) FROM pair_relationships WHERE state='pending' AND (user_a=? OR user_b=?)`,
		"a", "b")
}

func TestUserAnswersUseAnIndex(t *testing.T) {
	c := migrated(t)
	requireIndexUsed(t, c, "answers by user",
		`SELECT ra.* FROM round_answers ra JOIN rounds r ON r.id = ra.round_id WHERE ra.user_id = ?`,
		"user")
}

func TestMessagesInThreadAreOrderedByIndex(t *testing.T) {
	c := migrated(t)
	requireIndexUsed(t, c, "thread messages",
		`SELECT * FROM messages WHERE thread_id=? ORDER BY created_at, rowid`, "thread")
}

func TestPromptLookupUsesAnIndex(t *testing.T) {
	c := migrated(t)
	got := plan(t, c, `SELECT * FROM prompt_bank WHERE game_kind=? AND active=1`, "this_or_that")
	require.NotContains(t, got, "SCAN", "prompt_bank lookup should not scan; plan was: "+got)
}
