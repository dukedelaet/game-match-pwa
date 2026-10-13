package httpapi

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"gamematch/internal/config"
	"gamematch/internal/db"
	"gamematch/internal/game"
	"gamematch/internal/seed"
	"gamematch/internal/store"

	sqlite "modernc.org/sqlite"
)

// These list endpoints were exactly 4 + 6N, 4 + 5N and 4 + 4N statements: a
// pass per row for the other user, their profile, their photo, the pair, the
// thread and the block check. None had a LIMIT, so an account with a long
// history returned everything in one response.
//
// The counter registers a driver that wraps modernc.org/sqlite and tallies
// statements, so a test measures the real query load of a request instead of
// inferring it from the shape of the code.

type countingDriver struct {
	stmts *atomic.Int64
}

// countingConn implements Execer/Queryer directly so database/sql routes
// through the connection methods, which is where the tally happens.
type countingConn struct {
	driver.Conn
	stmts *atomic.Int64
}

func (d *countingDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: c, stmts: d.stmts}, nil
}

func (c *countingConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.stmts.Add(1)
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}

func (c *countingConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.stmts.Add(1)
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

// countingServer builds a server whose database counts statements.
func countingServer(t *testing.T) (*Server, *store.Store, *atomic.Int64) {
	t.Helper()
	var stmts atomic.Int64
	name := fmt.Sprintf("sqlite-count-%d", time.Now().UnixNano())
	sql.Register(name, &countingDriver{stmts: &stmts})

	dir := t.TempDir()
	cfg := config.Config{
		Addr:        "127.0.0.1:0",
		DataDir:     dir,
		Env:         "test",
		Debug:       true,
		HouseUserID: "00000000-0000-4000-8000-000000000001",
		DevOTP:      "123456",
	}

	// Same pragmas db.Open applies, on the counting driver.
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(ON)")
	conn, err := sqlx.Open(name, "file:"+filepath.Join(dir, "test.db")+"?"+q.Encode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, db.Migrate(context.Background(), conn))

	st := store.New(conn, cfg)
	require.NoError(t, seed.Run(context.Background(), st, cfg))
	srv := NewServer(st, game.NewEngine(st, cfg), game.NewMatcher(st, cfg), cfg)
	srv.DisableRateLimits = true
	return srv, st, &stmts
}

// seedMatches gives ownerID n mutual matches, each with a profile and a photo.
func seedMatches(t *testing.T, s *Server, st *store.Store, ownerID string, n int) {
	t.Helper()
	ctx := context.Background()
	now := store.NowTS()
	for i := 0; i < n; i++ {
		phone := fmt.Sprintf("+1557%06d", i)
		login(t, s, phone)
		otherID := mustUserIDByPhone(t, st, phone)

		_, err := st.DB.ExecContext(ctx, `
			INSERT INTO profiles (user_id, age, bio, created_at, updated_at)
			VALUES (?, 30, 'hello', ?, ?)`, otherID, now, now)
		require.NoError(t, err)
		_, err = st.DB.ExecContext(ctx, `
			INSERT INTO photos (id, user_id, path, moderation_state, created_at, updated_at)
			VALUES (?, ?, ?, 'ok', ?, ?)`, fmt.Sprintf("photo-%d", i), otherID,
			fmt.Sprintf("%s/%s/photo-%d.jpg", otherID, otherID, i), now, now)
		require.NoError(t, err)

		a, b := game.OrderedPair(ownerID, otherID)
		_, err = st.DB.ExecContext(ctx, `
			INSERT INTO matches (id, user_a, user_b, state, matched_at, created_at, updated_at)
			VALUES (?,?,?,'mutual',?,?,?)`, fmt.Sprintf("match-%d", i), a, b, now, now, now)
		require.NoError(t, err)
		_, err = st.DB.ExecContext(ctx, `
			INSERT INTO pair_relationships (id, user_a, user_b, state, created_at, updated_at)
			VALUES (?,?,?,'mutual',?,?)`, fmt.Sprintf("pair-%d", i), a, b, now, now)
		require.NoError(t, err)
	}
}

func listQueryCount(t *testing.T, s *Server, st *store.Store, counter *atomic.Int64, c *client, path string) int {
	t.Helper()
	before := counter.Load()
	rec := c.req(http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, rec.Code, "GET %s: %s", path, rec.Body.String())
	return int(counter.Load() - before)
}

const maxListItems = store.DefaultListLimit

func TestMatchesQueryCountDoesNotScaleWithRows(t *testing.T) {
	s, st, counter := countingServer(t)
	owner := login(t, s, "+15551111111")
	ownerID := mustUserIDByPhone(t, st, "+15551111111")
	seedMatches(t, s, st, ownerID, 10)

	n := listQueryCount(t, s, st, counter, owner, "/v1/matches")
	require.Less(t, n, 25, "GET /v1/matches issued %d statements for 10 matches; it must not scale with rows", n)
}

func TestThreadsQueryCountDoesNotScaleWithRows(t *testing.T) {
	s, st, counter := countingServer(t)
	owner := login(t, s, "+15551111111")
	ownerID := mustUserIDByPhone(t, st, "+15551111111")
	seedMatches(t, s, st, ownerID, 10)

	n := listQueryCount(t, s, st, counter, owner, "/v1/threads")
	require.Less(t, n, 25, "GET /v1/threads issued %d statements for 10 matches; it must not scale with rows", n)
}

func TestPairsQueryCountDoesNotScaleWithRows(t *testing.T) {
	s, st, counter := countingServer(t)
	owner := login(t, s, "+15551111111")
	ownerID := mustUserIDByPhone(t, st, "+15551111111")
	seedMatches(t, s, st, ownerID, 10)

	n := listQueryCount(t, s, st, counter, owner, "/v1/pairs")
	require.Less(t, n, 25, "GET /v1/pairs issued %d statements for 10 pairs; it must not scale with rows", n)
}

// None of these had a LIMIT: an account with a long history returned everything.
func TestListEndpointsAreBounded(t *testing.T) {
	s, st, _ := countingServer(t)
	owner := login(t, s, "+15551111111")
	ownerID := mustUserIDByPhone(t, st, "+15551111111")
	seedMatches(t, s, st, ownerID, 40)

	for _, path := range []string{"/v1/matches", "/v1/threads", "/v1/pairs"} {
		rec := owner.req(http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, rec.Code, path)

		var body struct {
			Items []json.RawMessage `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), path)
		require.LessOrEqualf(t, len(body.Items), maxListItems,
			"%s returned %d items; the response must be bounded by maxListItems (%d)", path, len(body.Items), maxListItems)
	}
}

// Paging must actually work, not just clamp.
func TestListEndpointsSupportPaging(t *testing.T) {
	s, st, _ := countingServer(t)
	owner := login(t, s, "+15551111111")
	ownerID := mustUserIDByPhone(t, st, "+15551111111")
	seedMatches(t, s, st, ownerID, 5)

	rec := owner.req(http.MethodGet, "/v1/matches?limit=2", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 2, "limit=2 must return two rows")

	rec = owner.req(http.MethodGet, "/v1/matches?limit=100000", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.LessOrEqual(t, len(body.Items), maxListItems)
}

// A nonsense limit must not 500 or silently return nothing.
func TestInvalidLimitFallsBackToTheDefault(t *testing.T) {
	s, st, _ := countingServer(t)
	owner := login(t, s, "+15551111111")
	ownerID := mustUserIDByPhone(t, st, "+15551111111")
	seedMatches(t, s, st, ownerID, 3)

	for _, q := range []string{"limit=abc", "limit=-5", "limit=0", "limit="} {
		rec := owner.req(http.MethodGet, "/v1/matches?"+q, nil)
		require.Equalf(t, http.StatusOK, rec.Code, "?%s returned %d: %s", q, rec.Code, rec.Body.String())

		var body struct {
			Items []json.RawMessage `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Lenf(t, body.Items, 3, "?%s should fall back to the default limit", q)
	}
}
