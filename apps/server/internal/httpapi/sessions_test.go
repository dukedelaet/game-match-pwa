package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

// A session ID is handed out in Completed.SessionID, ResumeToken, push payloads
// and the PWA URL. Every session endpoint must therefore check that the caller
// is one of the two players, not merely that they hold a valid account.
// mustUserIDByPhone resolves the user created by logging in with phone.
func mustUserIDByPhone(t *testing.T, st *store.Store, phone string) string {
	t.Helper()
	u, err := st.UserByPhoneHash(context.Background(), store.HashPhone(phone))
	require.NoError(t, err)
	return u.ID
}

func TestNonParticipantCannotReadSession(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	jordan := login(t, s, "+15552222222")
	stranger := login(t, s, "+15553333333")

	sessionID := insertSession(t, st, "this_or_that", "queue_1v1", "in_round",
		mustUserID(t, st, "Alex"), mustUserID(t, st, "Jordan"))

	code, _ := stranger.json(http.MethodGet, "/v1/sessions/"+sessionID, nil)
	require.Equal(t, http.StatusForbidden, code)

	// The players are unaffected.
	code, _ = alex.json(http.MethodGet, "/v1/sessions/"+sessionID, nil)
	require.Equal(t, http.StatusOK, code)
	code, _ = jordan.json(http.MethodGet, "/v1/sessions/"+sessionID, nil)
	require.Equal(t, http.StatusOK, code)
}

func TestNonParticipantCannotAnswer(t *testing.T) {
	s, st := newTestServer(t)
	login(t, s, "+15551111111")
	login(t, s, "+15552222222")
	stranger := login(t, s, "+15553333333")

	sessionID := insertSession(t, st, "this_or_that", "queue_1v1", "in_round",
		mustUserID(t, st, "Alex"), mustUserID(t, st, "Jordan"))

	code, _ := stranger.json(http.MethodPost, "/v1/sessions/"+sessionID+"/answer",
		map[string]any{"payload": map[string]any{"choices": []string{"a", "b"}}})
	require.Equal(t, http.StatusForbidden, code)

	// No answer row was written on the stranger's behalf.
	var n int
	require.NoError(t, st.DB.GetContext(context.Background(), &n,
		`SELECT count(*) FROM round_answers ra JOIN rounds r ON r.id = ra.round_id
		 WHERE r.session_id = ? AND ra.user_id = ?`,
		sessionID, mustUserIDByPhone(t, st, "+15553333333")))
	require.Zero(t, n)
}

func TestNonParticipantCannotLeave(t *testing.T) {
	s, st := newTestServer(t)
	login(t, s, "+15551111111")
	login(t, s, "+15552222222")
	stranger := login(t, s, "+15553333333")

	sessionID := insertSession(t, st, "this_or_that", "queue_1v1", "in_round",
		mustUserID(t, st, "Alex"), mustUserID(t, st, "Jordan"))

	code, _ := stranger.json(http.MethodPost, "/v1/sessions/"+sessionID+"/leave", nil)
	require.Equal(t, http.StatusForbidden, code)

	ses, err := st.GetSession(context.Background(), sessionID)
	require.NoError(t, err)
	require.Equal(t, "in_round", ses.State, "a stranger must not be able to forfeit the game")
}

// TouchPoll is a bare UPDATE ... WHERE user_id = ?, which affects zero rows for
// a non-participant and reports no error. That silence is what let an
// unauthorized poll through Heartbeat.
func TestTouchPollReportsMissingParticipant(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	login(t, s, "+15553333333")

	sessionID := insertSession(t, st, "this_or_that", "queue_1v1", "in_round",
		mustUserID(t, st, "Alex"), mustUserID(t, st, "Jordan"))

	err := st.TouchPoll(ctx, sessionID, mustUserIDByPhone(t, st, "+15553333333"))
	require.ErrorIs(t, err, store.ErrNoParticipant)

	require.NoError(t, st.TouchPoll(ctx, sessionID, mustUserID(t, st, "Alex")))
}
