package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func (c *client) upload(path, field, filename string, content []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile(field, filename)
	require.NoError(c.t, err)
	_, err = fw.Write(content)
	require.NoError(c.t, err)
	require.NoError(c.t, w.Close())

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

// insertSession writes a session row and its participants directly.
func insertSession(t *testing.T, st *store.Store, kind, mode, state string, userIDs ...string) string {
	t.Helper()
	id := uuid.NewString()
	now := store.NowTS()
	_, err := st.DB.Exec(`INSERT INTO game_sessions (id,kind,mode,state,config,current_round,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, id, kind, mode, state, `{"rounds":8,"countdown_ms":3000,"reveal_ms":4000}`, 0, now, now)
	require.NoError(t, err)
	for i, uid := range userIDs {
		_, err := st.DB.Exec(`INSERT INTO session_participants (session_id,user_id,seat,joined_at,last_poll_at)
			VALUES (?,?,?,?,?)`, id, uid, i, now, now)
		require.NoError(t, err)
	}
	return id
}

func TestUploadPhotoAndServe(t *testing.T) {
	s, _ := newTestServer(t)
	alex := login(t, s, "+15551111111")

	rec := alex.upload("/v1/me/photos", "photo", "me.png", pngBytes(t, 2000, 1200))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	photo := body["photo"]
	require.NotEmpty(t, photo["id"])
	require.Equal(t, "/v1/photos/"+photo["id"], photo["url"])

	// the stored file is served back as JPEG
	grec := alex.req(http.MethodGet, photo["url"], nil)
	require.Equal(t, http.StatusOK, grec.Code)
	require.True(t, strings.HasPrefix(grec.Body.String(), "\xff\xd8"), "served bytes should be JPEG")

	// garbage is rejected
	bad := alex.upload("/v1/me/photos", "photo", "me.png", []byte("not an image"))
	require.Equal(t, http.StatusUnprocessableEntity, bad.Code)
}

func TestPhotoRequiresAuth(t *testing.T) {
	s, _ := newTestServer(t)
	rec := newClient(t, s).req(http.MethodGet, "/v1/photos/some-id", nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLogoutInvalidatesSession(t *testing.T) {
	s, _ := newTestServer(t)
	alex := login(t, s, "+15551111111")

	code, _ := alex.json(http.MethodPost, "/v1/auth/logout", nil)
	require.Equal(t, http.StatusOK, code)

	code, _ = alex.json(http.MethodGet, "/v1/auth/session", nil)
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestDemoOAuthCreatesAndReusesUser(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	c := newClient(t, s)
	code, body := c.json(http.MethodPost, "/v1/auth/oauth/apple", nil)
	require.Equal(t, http.StatusOK, code)
	user := body["user"].(map[string]any)
	require.Equal(t, "Apple Demo", user["name"])
	firstID := user["id"].(string)

	// a second sign-in reuses the same account
	_, body2 := newClient(t, s).json(http.MethodPost, "/v1/auth/oauth/apple", nil)
	require.Equal(t, firstID, body2["user"].(map[string]any)["id"])

	stored, err := st.UserByEmail(ctx, "apple-demo@gamematch.local")
	require.NoError(t, err)
	require.Equal(t, firstID, stored.ID)

	// unknown providers are rejected
	code, _ = newClient(t, s).json(http.MethodPost, "/v1/auth/oauth/myspace", nil)
	require.Equal(t, http.StatusUnprocessableEntity, code)
}

func TestGetMeGamesAndLegal(t *testing.T) {
	s, _ := newTestServer(t)
	alex := login(t, s, "+15551111111")

	code, body := alex.json(http.MethodGet, "/v1/me", nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "Alex", body["user"].(map[string]any)["name"])

	code, games := alex.json(http.MethodGet, "/v1/games", nil)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, games["items"].([]any), 3)

	code, legal := alex.json(http.MethodGet, "/v1/legal", nil)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, legal["terms"])
	require.NotEmpty(t, legal["privacy"])
}

func TestQueueLeaveRemovesWaitingEntry(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")

	_, status := alex.json(http.MethodPost, "/v1/queue", map[string]any{"gameKind": "this_or_that"})
	require.Equal(t, "waiting", status["state"])

	code, _ := alex.json(http.MethodDelete, "/v1/queue", nil)
	require.Equal(t, http.StatusOK, code)

	entry, err := st.QueueEntryForUser(ctx, alexID)
	require.NoError(t, err)
	require.Nil(t, entry)

	_, status = alex.json(http.MethodGet, "/v1/queue/status", nil)
	require.Equal(t, "idle", status["state"])
}

func TestSessionLeaveForfeits(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	sessionID := insertSession(t, st, "this_or_that", "queue_1v1", "in_round", alexID, jordanID)

	alex := login(t, s, "+15551111111")
	code, _ := alex.json(http.MethodPost, "/v1/sessions/"+sessionID+"/leave", nil)
	require.Equal(t, http.StatusOK, code)

	got, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, "forfeit", got.State)
	require.NotNil(t, got.EndedAt)
}

func TestRematchCreatesInvite(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	insertPair(t, st, alexID, jordanID, "open_play", nil)
	sessionID := insertSession(t, st, "this_or_that", "invite", "completed", alexID, jordanID)

	code, body := alex.json(http.MethodPost, "/v1/sessions/"+sessionID+"/rematch", nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "this_or_that", body["gameKind"])
	require.Equal(t, true, body["fromMe"], "the caller created the invite")
	require.Equal(t, jordanID, body["other"].(map[string]any)["id"])

	// a practice session cannot be rematched
	practiceID := insertSession(t, st, "this_or_that", "practice", "completed", alexID, jordanID)
	code, _ = alex.json(http.MethodPost, "/v1/sessions/"+practiceID+"/rematch", nil)
	require.Equal(t, http.StatusNotFound, code)

	// a stranger cannot rematch
	stranger := login(t, s, "+15559999999")
	code, _ = stranger.json(http.MethodPost, "/v1/sessions/"+sessionID+"/rematch", nil)
	require.Equal(t, http.StatusNotFound, code)
}

func TestModReportsRequiresStaffAndLists(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	code, _ := alex.json(http.MethodPost, "/v1/reports", map[string]any{"userId": jordanID, "reason": "harassment"})
	require.Equal(t, http.StatusOK, code)

	code, _ = alex.json(http.MethodGet, "/v1/internal/mod/reports", nil)
	require.Equal(t, http.StatusForbidden, code, "a normal user must not read reports")

	staff := login(t, s, "+15550000000")
	code, body := staff.json(http.MethodGet, "/v1/internal/mod/reports", nil)
	require.Equal(t, http.StatusOK, code)
	items := body["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, "harassment", items[0].(map[string]any)["reason"])
	require.Equal(t, alexID, items[0].(map[string]any)["reporter_id"])
}
