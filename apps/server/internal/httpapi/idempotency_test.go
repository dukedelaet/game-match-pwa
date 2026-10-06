package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func (c *client) reqWithHeader(method, path string, body any, header, value string) *httptest.ResponseRecorder {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(c.t, err)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(header, value)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func countRows(t *testing.T, st interface {
	Get(dest any, query string, args ...any) error
}, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, st.Get(&n, query, args...))
	return n
}

func TestInviteIsIdempotent(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	insertPair(t, st, alexID, jordanID, "open_play", nil)

	body := map[string]any{"targetUserId": jordanID, "gameKind": "this_or_that"}
	first := alex.reqWithHeader(http.MethodPost, "/v1/invites", body, "Idempotency-Key", "invite-1")
	require.Equal(t, http.StatusOK, first.Code)
	second := alex.reqWithHeader(http.MethodPost, "/v1/invites", body, "Idempotency-Key", "invite-1")
	require.Equal(t, http.StatusOK, second.Code)

	require.JSONEq(t, first.Body.String(), second.Body.String(), "replay returns the original body")
	require.Equal(t, "true", second.Header().Get("Idempotency-Replayed"))

	var invite struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &invite))

	require.Equal(t, 1, countRows(t, st.DB, `SELECT count(*) FROM invites WHERE from_user_id = ?`, alexID),
		"only one invite is created")
	require.Equal(t, 1, countRows(t, st.DB, `SELECT count(*) FROM idempotency_keys WHERE user_id = ?`, alexID))
}

func TestIdempotencyKeyConflictWhenBodyDiffers(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	insertPair(t, st, alexID, jordanID, "open_play", nil)

	ok := alex.reqWithHeader(http.MethodPost, "/v1/invites",
		map[string]any{"targetUserId": jordanID, "gameKind": "this_or_that"}, "Idempotency-Key", "k1")
	require.Equal(t, http.StatusOK, ok.Code)

	conflict := alex.reqWithHeader(http.MethodPost, "/v1/invites",
		map[string]any{"targetUserId": jordanID, "gameKind": "twenty_questions"}, "Idempotency-Key", "k1")
	require.Equal(t, http.StatusConflict, conflict.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(conflict.Body.Bytes(), &body))
	require.Equal(t, "idempotency_conflict", body["error"].(map[string]any)["code"])
}

func TestMessagesAreIdempotent(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	_, _, threadID := seedMutualPair(t, st, alexID, jordanID)

	body := map[string]any{"body": "only once"}
	first := alex.reqWithHeader(http.MethodPost, "/v1/threads/"+threadID+"/messages", body, "Idempotency-Key", "msg-1")
	require.Equal(t, http.StatusOK, first.Code)
	second := alex.reqWithHeader(http.MethodPost, "/v1/threads/"+threadID+"/messages", body, "Idempotency-Key", "msg-1")
	require.Equal(t, http.StatusOK, second.Code)

	require.JSONEq(t, first.Body.String(), second.Body.String())
	require.Equal(t, 1, countRows(t, st.DB, `SELECT count(*) FROM messages WHERE thread_id = ?`, threadID))
}

func TestIdempotencyIgnoredWithoutHeader(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	insertPair(t, st, alexID, jordanID, "open_play", nil)

	body := map[string]any{"targetUserId": jordanID, "gameKind": "this_or_that"}
	first := alex.req(http.MethodPost, "/v1/invites", body)
	second := alex.req(http.MethodPost, "/v1/invites", body)
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, second.Code)
	require.NotEqual(t, first.Body.String(), second.Body.String(), "no header means a fresh action")
}
