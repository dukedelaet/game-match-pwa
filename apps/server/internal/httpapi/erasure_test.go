package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

// DELETE /v1/me is presented to users as deleting their account, so it has to
// erase the identifying data: date of birth, the phone number in cleartext,
// the coarse location, and the messages they sent. These tests read the raw
// tables rather than the DTO layer, because the DTO layer is careful about
// what it exposes -- which is why the delete could leave everything behind
// unnoticed.

// openThread wires a thread straight between two users so a message can be
// sent without playing a whole game first.
func openThread(t *testing.T, st *store.Store, a, b string) string {
	t.Helper()
	ctx := context.Background()
	now := store.NowTS()
	matchID := uuid.NewString()
	threadID := uuid.NewString()
	_, err := st.DB.ExecContext(ctx, `
		INSERT INTO matches (id,user_a,user_b,state,matched_at,created_at,updated_at)
		VALUES (?,?,?,'mutual',?,?,?)`, matchID, a, b, now, now, now)
	require.NoError(t, err)
	_, err = st.DB.ExecContext(ctx, `
		INSERT INTO chat_threads (id,match_id,state,created_at,updated_at)
		VALUES (?,?,'open',?,?)`, threadID, matchID, now, now)
	require.NoError(t, err)
	return threadID
}

func seedIdentity(t *testing.T, st *store.Store, userID string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.UpsertPrivatePhone(ctx, userID, "+15551111111"))
	_, err := st.DB.ExecContext(ctx,
		`UPDATE users SET dob='1994-03-02', approx_geohash='9q8yy' WHERE id=?`, userID)
	require.NoError(t, err)
}

func TestDeleteAccountErasesIdentifyingData(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	alex := login(t, s, "+15551111111")
	alexID := mustUserIDByPhone(t, st, "+15551111111")
	seedIdentity(t, st, alexID)

	rec := alex.upload("/v1/me/photos", "photo", "me.png", pngBytes(t, 2000, 1200))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	code, body := alex.json(http.MethodDelete, "/v1/me", nil)
	require.Equal(t, http.StatusOK, code, "delete body: %v", body)

	t.Run("cleartext phone number is gone", func(t *testing.T) {
		var n int
		require.NoError(t, st.DB.GetContext(ctx, &n,
			`SELECT count(*) FROM user_private WHERE user_id = ? AND phone_e164 IS NOT NULL`, alexID))
		require.Zero(t, n)
	})

	t.Run("date of birth is gone", func(t *testing.T) {
		var dob *string
		require.NoError(t, st.DB.GetContext(ctx, &dob, `SELECT dob FROM users WHERE id = ?`, alexID))
		require.Nil(t, dob)
	})

	t.Run("coarse location is gone", func(t *testing.T) {
		var geo *string
		require.NoError(t, st.DB.GetContext(ctx, &geo, `SELECT approx_geohash FROM users WHERE id = ?`, alexID))
		require.Nil(t, geo)
	})

	t.Run("phone hash is released so the number can be reused", func(t *testing.T) {
		var hash *string
		require.NoError(t, st.DB.GetContext(ctx, &hash, `SELECT phone_e164_hash FROM users WHERE id = ?`, alexID))
		require.Nil(t, hash, "phone_e164_hash is a UNIQUE stable identifier; keeping it means the number can never re-register")
	})

	t.Run("photo rows do not outlive their files", func(t *testing.T) {
		var n int
		require.NoError(t, st.DB.GetContext(ctx, &n, `SELECT count(*) FROM photos WHERE user_id = ?`, alexID))
		require.Zero(t, n)
	})

	t.Run("account is marked deleted", func(t *testing.T) {
		var status string
		require.NoError(t, st.DB.GetContext(ctx, &status, `SELECT status FROM users WHERE id = ?`, alexID))
		require.Equal(t, "deleted", status)
	})
}

func TestDeleteAccountRemovesSentMessages(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	alex := login(t, s, "+15551111111")
	jordan := login(t, s, "+15552222222")
	alexID := mustUserIDByPhone(t, st, "+15551111111")
	jordanID := mustUserIDByPhone(t, st, "+15552222222")

	threadID := openThread(t, st, alexID, jordanID)
	// Both sides write, so the deletion has something to selectively remove.
	code, body := jordan.json(http.MethodPost, "/v1/threads/"+threadID+"/messages",
		map[string]any{"body": "nice to meet you"})
	require.Equal(t, http.StatusOK, code, "jordan send body: %v", body)
	code, body = alex.json(http.MethodPost, "/v1/threads/"+threadID+"/messages",
		map[string]any{"body": "something personal"})
	require.Equal(t, http.StatusOK, code, "alex send body: %v", body)

	code, _ = alex.json(http.MethodDelete, "/v1/me", nil)
	require.Equal(t, http.StatusOK, code)

	var n int
	require.NoError(t, st.DB.GetContext(ctx, &n,
		`SELECT count(*) FROM messages WHERE sender_id = ?`, alexID))
	require.Zero(t, n, "the deleted user's messages must be erased")

	require.NoError(t, st.DB.GetContext(ctx, &n,
		`SELECT count(*) FROM messages WHERE sender_id = ?`, jordanID))
	require.Positive(t, n, "the other party's messages must survive")
}

func TestDeleteAccountRevokesSessions(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserIDByPhone(t, st, "+15551111111")

	code, _ := alex.json(http.MethodDelete, "/v1/me", nil)
	require.Equal(t, http.StatusOK, code)

	var n int
	require.NoError(t, st.DB.GetContext(context.Background(), &n,
		`SELECT count(*) FROM sessions WHERE user_id = ?`, alexID))
	require.Zero(t, n, "a deleted account must not keep API access until its token expires")
}

func TestDeleteAccountBlockedByLegalHold(t *testing.T) {
	s, st := newTestServer(t)

	alex := login(t, s, "+15551111111")
	alexID := mustUserIDByPhone(t, st, "+15551111111")

	code, body := alex.json(http.MethodPost, "/v1/reports", map[string]any{
		"reason": "csam", "userId": alexID,
	})
	require.Equal(t, http.StatusOK, code, "report body: %v", body)

	code, body = alex.json(http.MethodDelete, "/v1/me", nil)
	require.Equal(t, http.StatusConflict, code)
	require.Equal(t, "legal_hold", body["error"].(map[string]any)["code"])
}
