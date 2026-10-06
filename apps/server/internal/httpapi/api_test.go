package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/config"
	"gamematch/internal/db"
	"gamematch/internal/game"
	"gamematch/internal/seed"
	"gamematch/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), conn))
	cfg := config.Config{
		Addr:        "127.0.0.1:0",
		DataDir:     dir,
		Env:         "test",
		Debug:       true,
		HouseUserID: "00000000-0000-4000-8000-000000000001",
		DevOTP:      "123456",
	}
	st := store.New(conn, cfg)
	require.NoError(t, seed.Run(context.Background(), st, cfg))
	engine := game.NewEngine(st, cfg)
	matcher := game.NewMatcher(st, cfg)
	return NewServer(st, engine, matcher, cfg), st
}

type client struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
}

func newClient(t *testing.T, s *Server) *client {
	return &client{t: t, h: s.Router()}
}

func (c *client) req(method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(c.t, err)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == sessionCookie {
			c.cookie = ck
		}
	}
	return rec
}

func (c *client) json(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	rec := c.req(method, path, body)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func login(t *testing.T, s *Server, phone string) *client {
	t.Helper()
	c := newClient(t, s)
	code, _ := c.json(http.MethodPost, "/v1/auth/otp/start", map[string]string{"phone": phone})
	require.Equal(t, http.StatusOK, code)
	code, body := c.json(http.MethodPost, "/v1/auth/otp/verify", map[string]string{"phone": phone, "code": "123456"})
	require.Equal(t, http.StatusOK, code, "verify body: %v", body)
	require.NotNil(t, c.cookie)
	return c
}

func TestHealthz(t *testing.T) {
	s, _ := newTestServer(t)
	rec := newClient(t, s).req(http.MethodGet, "/v1/healthz", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok":true}`, rec.Body.String())
}

func TestSessionUnauthenticated(t *testing.T) {
	s, _ := newTestServer(t)
	code, body := newClient(t, s).json(http.MethodGet, "/v1/auth/session", nil)
	require.Equal(t, http.StatusUnauthorized, code)
	require.Equal(t, "unauth", body["error"].(map[string]any)["code"])
}

func TestOtpLoginAndSession(t *testing.T) {
	s, _ := newTestServer(t)
	c := login(t, s, "+15551111111")

	code, body := c.json(http.MethodGet, "/v1/auth/session", nil)
	require.Equal(t, http.StatusOK, code)
	user := body["user"].(map[string]any)
	require.Equal(t, "Alex", user["name"])
	require.Equal(t, "done", user["onboardingStep"])
}

func TestCatalogsHideCoordinates(t *testing.T) {
	s, _ := newTestServer(t)
	c := login(t, s, "+15551111111")
	code, body := c.json(http.MethodGet, "/v1/catalogs", nil)
	require.Equal(t, http.StatusOK, code)
	metros := body["metros"].([]any)
	require.NotEmpty(t, metros)
	first := metros[0].(map[string]any)
	require.NotContains(t, first, "centroid_lat")
	require.NotContains(t, first, "adjacent_ids")
	require.Len(t, body["intents"], 4)
}

func TestPatchMeRejectsUnder18(t *testing.T) {
	s, _ := newTestServer(t)
	c := login(t, s, "+15551111111")
	code, body := c.json(http.MethodPatch, "/v1/me", map[string]any{"dob": "2015-01-01"})
	require.Equal(t, http.StatusForbidden, code)
	require.Equal(t, "age", body["error"].(map[string]any)["code"])
}

func TestPatchMeUpdatesIncognitoAndOnboarding(t *testing.T) {
	s, _ := newTestServer(t)
	c := login(t, s, "+15551111111")
	code, body := c.json(http.MethodPatch, "/v1/me", map[string]any{"incognito": true, "onboarding_step": "done"})
	require.Equal(t, http.StatusOK, code)
	user := body["user"].(map[string]any)
	require.Equal(t, true, user["incognito"])
	require.Equal(t, "active", user["status"])
}

func TestQueueMatchesTwoPlayers(t *testing.T) {
	s, _ := newTestServer(t)
	alex := login(t, s, "+15551111111")
	jordan := login(t, s, "+15552222222")

	_, _ = alex.json(http.MethodPost, "/v1/queue", map[string]any{"gameKind": "this_or_that"})
	_, _ = jordan.json(http.MethodPost, "/v1/queue", map[string]any{"gameKind": "this_or_that"})

	_, aStatus := alex.json(http.MethodGet, "/v1/queue/status", nil)
	_, jStatus := jordan.json(http.MethodGet, "/v1/queue/status", nil)
	require.Equal(t, "matched", aStatus["state"])
	require.Equal(t, "matched", jStatus["state"])
	require.Equal(t, aStatus["sessionId"], jStatus["sessionId"])
	require.NotEmpty(t, aStatus["sessionId"])
}

// forceDeadlines backdates the session's countdown/answer deadlines so the
// engine advances without waiting on real timers.
func forceDeadlines(t *testing.T, st *store.Store, sessionID string) {
	t.Helper()
	past := store.FmtTS(time.Now().UTC().Add(-2 * time.Second))
	_, err := st.DB.Exec(`UPDATE game_sessions SET started_at=?, answer_by=? WHERE id=?`, past, past, sessionID)
	require.NoError(t, err)
	_, err = st.DB.Exec(`UPDATE rounds SET answer_by=? WHERE session_id=?`, past, sessionID)
	require.NoError(t, err)
}

func TestFullThisOrThatGameThenConnect(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	jordan := login(t, s, "+15552222222")

	_, _ = alex.json(http.MethodPost, "/v1/queue", map[string]any{"gameKind": "this_or_that"})
	_, _ = jordan.json(http.MethodPost, "/v1/queue", map[string]any{"gameKind": "this_or_that"})
	_, aStatus := alex.json(http.MethodGet, "/v1/queue/status", nil)
	sessionID := aStatus["sessionId"].(string)
	require.NotEmpty(t, sessionID)

	code, _ := alex.json(http.MethodPost, "/v1/sessions/"+sessionID+"/join", nil)
	require.Equal(t, http.StatusOK, code)
	code, _ = jordan.json(http.MethodPost, "/v1/sessions/"+sessionID+"/join", nil)
	require.Equal(t, http.StatusOK, code)

	forceDeadlines(t, st, sessionID)

	var final map[string]any
	for round := 1; round <= 8; round++ {
		_, view := alex.json(http.MethodGet, "/v1/sessions/"+sessionID, nil)
		require.Equal(t, "in_round", view["state"], "round %d", round)

		_, _ = alex.json(http.MethodPost, "/v1/sessions/"+sessionID+"/answer", map[string]any{"payload": map[string]string{"choice": "left"}})
		_, reveal := jordan.json(http.MethodPost, "/v1/sessions/"+sessionID+"/answer", map[string]any{"payload": map[string]string{"choice": "left"}})
		require.Equal(t, "reveal_round", reveal["state"], "round %d", round)
		require.Equal(t, true, reveal["reveal"].(map[string]any)["same"])

		forceDeadlines(t, st, sessionID)
		_, view = alex.json(http.MethodGet, "/v1/sessions/"+sessionID, nil)
		final = view
	}
	require.Equal(t, "completed", final["state"])

	completed := final["completed"].(map[string]any)
	require.NotNil(t, completed["pairId"])
	require.NotNil(t, completed["snapshot"])
	pairID := completed["pairId"].(string)

	code, _ = alex.json(http.MethodPost, "/v1/pairs/"+pairID+"/connect", map[string]any{"action": "connect"})
	require.Equal(t, http.StatusOK, code)
	code, _ = jordan.json(http.MethodPost, "/v1/pairs/"+pairID+"/connect", map[string]any{"action": "connect"})
	require.Equal(t, http.StatusOK, code)

	_, pairs := alex.json(http.MethodGet, "/v1/pairs", nil)
	items := pairs["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, "mutual", items[0].(map[string]any)["state"])

	_, threads := alex.json(http.MethodGet, "/v1/threads", nil)
	threadItems := threads["items"].([]any)
	require.Len(t, threadItems, 1)
	threadID := threadItems[0].(map[string]any)["threadId"].(string)

	_, messages := alex.json(http.MethodGet, "/v1/threads/"+threadID+"/messages", nil)
	msgs := messages["items"].([]any)
	require.NotEmpty(t, msgs)
	require.Equal(t, "icebreaker", msgs[0].(map[string]any)["kind"])

	// first message awards XP and does not lock the thread
	code, _ = alex.json(http.MethodPost, "/v1/threads/"+threadID+"/messages", map[string]any{"body": "hey"})
	require.Equal(t, http.StatusOK, code)

	// blocking hides the pair
	code, _ = alex.json(http.MethodPost, "/v1/blocks", map[string]any{"userId": mustUserID(t, st, "Jordan")})
	require.Equal(t, http.StatusOK, code)
	_, pairs = alex.json(http.MethodGet, "/v1/pairs", nil)
	require.Empty(t, pairs["items"].([]any))
}

func TestBlockHidesPairAndLocksChat(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	jordanID := mustUserID(t, st, "Jordan")

	code, _ := alex.json(http.MethodPost, "/v1/blocks", map[string]any{"userId": jordanID})
	require.Equal(t, http.StatusOK, code)
	_, pairs := alex.json(http.MethodGet, "/v1/pairs", nil)
	require.Empty(t, pairs["items"].([]any))

	code, _ = alex.json(http.MethodDelete, "/v1/blocks/"+jordanID, nil)
	require.Equal(t, http.StatusOK, code)
}

func TestReportCsamHidesSubjectAndCreatesHold(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	jordanID := mustUserID(t, st, "Jordan")

	code, _ := alex.json(http.MethodPost, "/v1/reports", map[string]any{"userId": jordanID, "reason": "csam"})
	require.Equal(t, http.StatusOK, code)

	jordan, err := st.GetUser(context.Background(), jordanID)
	require.NoError(t, err)
	require.Equal(t, "hidden", jordan.Status)
	hold, err := st.HasActiveLegalHold(context.Background(), jordanID)
	require.NoError(t, err)
	require.True(t, hold)
}

func TestStaffAllowlistRequiresAdmin(t *testing.T) {
	s, _ := newTestServer(t)
	alex := login(t, s, "+15551111111")
	code, _ := alex.json(http.MethodGet, "/v1/staff/allowlist", nil)
	require.Equal(t, http.StatusForbidden, code)

	staff := login(t, s, "+15550000000")
	code, body := staff.json(http.MethodGet, "/v1/staff/allowlist", nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, float64(3), body["count"])
	require.Equal(t, true, body["publicSignup"])
}

func mustUserID(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	u, err := st.UserByName(context.Background(), name)
	require.NoError(t, err)
	return u.ID
}
