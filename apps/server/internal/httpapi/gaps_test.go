package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"gamematch/internal/game"
	"gamematch/internal/store"
)

func mustUser(t *testing.T, st *store.Store, name string) store.User {
	t.Helper()
	u, err := st.UserByName(context.Background(), name)
	require.NoError(t, err)
	return u
}

// insertPair writes a pair row directly so tests can start from a known state
// without playing a full game.
func insertPair(t *testing.T, st *store.Store, aID, bID, state string, cooldown *string) string {
	t.Helper()
	a, b := game.OrderedPair(aID, bID)
	id := uuid.NewString()
	now := store.NowTS()
	_, err := st.DB.Exec(`INSERT INTO pair_relationships (id,user_a,user_b,state,a_action,b_action,cooldown_until,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, id, a, b, state, "none", "none", cooldown, now, now)
	require.NoError(t, err)
	return id
}

// seedMutualPair writes a mutual pair, its match, and an open thread.
func seedMutualPair(t *testing.T, st *store.Store, aID, bID string) (pairID, matchID, threadID string) {
	t.Helper()
	a, b := game.OrderedPair(aID, bID)
	now := store.NowTS()
	pairID, matchID, threadID = uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err := st.DB.Exec(`INSERT INTO pair_relationships (id,user_a,user_b,state,a_action,b_action,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, pairID, a, b, "mutual", "connect", "connect", now, now)
	require.NoError(t, err)
	_, err = st.DB.Exec(`INSERT INTO matches (id,user_a,user_b,state,matched_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?)`, matchID, a, b, "mutual", now, now, now)
	require.NoError(t, err)
	_, err = st.DB.Exec(`INSERT INTO chat_threads (id,match_id,state,created_at,updated_at)
		VALUES (?,?,?,?,?)`, threadID, matchID, "open", now, now)
	require.NoError(t, err)
	return pairID, matchID, threadID
}

func TestUnmatchLocksThreadAndSetsCooldown(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	pairID, matchID, threadID := seedMutualPair(t, st, alexID, jordanID)

	code, _ := alex.json(http.MethodPost, "/v1/pairs/"+pairID+"/unmatch", nil)
	require.Equal(t, http.StatusOK, code)

	p, err := st.PairByID(ctx, pairID)
	require.NoError(t, err)
	require.Equal(t, "unmatched", p.State)
	require.NotNil(t, p.CooldownUntil)
	require.True(t, store.ParseTS(*p.CooldownUntil).After(time.Now().UTC()))

	m, err := st.MatchByID(ctx, matchID)
	require.NoError(t, err)
	require.Equal(t, "unmatched", m.State)
	require.NotNil(t, m.UnmatchedBy)
	require.Equal(t, alexID, *m.UnmatchedBy)

	th, err := st.ThreadByID(ctx, threadID)
	require.NoError(t, err)
	require.Equal(t, "locked", th.State)

	// the match is no longer mutual, so the thread disappears from the list
	_, threads := alex.json(http.MethodGet, "/v1/threads", nil)
	require.Empty(t, threads["items"].([]any))
}

func TestQuietPassHidesPairFromNonPasser(t *testing.T) {
	s, st := newTestServer(t)
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	// Alex passed; Jordan connected. Alex sees the row, Jordan must not.
	a, b := game.OrderedPair(alexID, jordanID)
	pairID := uuid.NewString()
	now := store.NowTS()
	aAction, bAction := "pass", "connect"
	if a != alexID {
		aAction, bAction = "connect", "pass"
	}
	_, err := st.DB.Exec(`INSERT INTO pair_relationships (id,user_a,user_b,state,a_action,b_action,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, pairID, a, b, "closed", aAction, bAction, now, now)
	require.NoError(t, err)

	alex := login(t, s, "+15551111111")
	jordan := login(t, s, "+15552222222")

	code, _ := alex.json(http.MethodGet, "/v1/pairs/"+pairID, nil)
	require.Equal(t, http.StatusOK, code, "the passer may see their own pass")
	code, _ = jordan.json(http.MethodGet, "/v1/pairs/"+pairID, nil)
	require.Equal(t, http.StatusNotFound, code, "the other party sees nothing")

	_, alexPairs := alex.json(http.MethodGet, "/v1/pairs", nil)
	require.Len(t, alexPairs["items"].([]any), 1)
	_, jordanPairs := jordan.json(http.MethodGet, "/v1/pairs", nil)
	require.Empty(t, jordanPairs["items"].([]any))
}

func TestInviteRequiresExistingPair(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	jordanID := mustUserID(t, st, "Jordan")

	code, body := alex.json(http.MethodPost, "/v1/invites", map[string]any{"targetUserId": jordanID, "gameKind": "this_or_that"})
	require.Equal(t, http.StatusConflict, code)
	require.Equal(t, "unknown", body["error"].(map[string]any)["code"])
}

func TestInviteBlockedByCooldown(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	future := store.FmtTS(time.Now().UTC().Add(24 * time.Hour))
	insertPair(t, st, alexID, jordanID, "closed", &future)

	code, body := alex.json(http.MethodPost, "/v1/invites", map[string]any{"targetUserId": jordanID, "gameKind": "this_or_that"})
	require.Equal(t, http.StatusConflict, code)
	require.Equal(t, "cooldown", body["error"].(map[string]any)["code"])
}

func TestInviteHonorsBlock(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	insertPair(t, st, alexID, jordanID, "open_play", nil)
	require.NoError(t, st.InsertBlock(ctx, alexID, jordanID))

	code, _ := alex.json(http.MethodPost, "/v1/invites", map[string]any{"targetUserId": jordanID, "gameKind": "this_or_that"})
	require.Equal(t, http.StatusNotFound, code)
}

func TestInviteAcceptStartsSession(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	jordan := login(t, s, "+15552222222")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	insertPair(t, st, alexID, jordanID, "open_play", nil)

	code, invite := alex.json(http.MethodPost, "/v1/invites", map[string]any{"targetUserId": jordanID, "gameKind": "this_or_that"})
	require.Equal(t, http.StatusOK, code)
	inviteID := invite["id"].(string)
	require.Equal(t, true, invite["fromMe"], "the creator sees fromMe=true")

	// the recipient sees it on /home
	_, home := jordan.json(http.MethodGet, "/v1/home", nil)
	require.Len(t, home["invites"].([]any), 1)

	code, accepted := jordan.json(http.MethodPost, "/v1/invites/"+inviteID+"/accept", nil)
	require.Equal(t, http.StatusOK, code)
	sessionID := accepted["sessionId"].(string)

	session, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, "invite", session.Mode)
	require.Equal(t, "this_or_that", session.Kind)
	parts, err := st.Participants(ctx, sessionID)
	require.NoError(t, err)
	require.Len(t, parts, 2)
}

func TestInviteDeclineOnlyByRecipient(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	jordan := login(t, s, "+15552222222")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	insertPair(t, st, alexID, jordanID, "open_play", nil)
	_, invite := alex.json(http.MethodPost, "/v1/invites", map[string]any{"targetUserId": jordanID, "gameKind": "this_or_that"})
	inviteID := invite["id"].(string)

	// the sender cannot decline their own invite
	code, _ := alex.json(http.MethodPost, "/v1/invites/"+inviteID+"/decline", nil)
	require.Equal(t, http.StatusNotFound, code)

	code, _ = jordan.json(http.MethodPost, "/v1/invites/"+inviteID+"/decline", nil)
	require.Equal(t, http.StatusOK, code)

	inv, err := st.GetInvite(ctx, inviteID)
	require.NoError(t, err)
	require.Equal(t, "declined", inv.State)

	_, home := alex.json(http.MethodGet, "/v1/home", nil)
	require.Empty(t, home["invites"].([]any))
}

func TestDeleteMeAnonymizesAndRemovesPhotos(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	u := mustUser(t, st, "Alex")

	// put a photo row and its file on disk
	require.NoError(t, os.MkdirAll(filepath.Join(s.PhotosDir, u.ID), 0o755))
	rel := filepath.ToSlash(filepath.Join(u.ID, "p1.jpg"))
	require.NoError(t, os.WriteFile(filepath.Join(s.PhotosDir, rel), []byte("jpegbytes"), 0o644))
	now := store.NowTS()
	_, err := st.DB.Exec(`INSERT INTO photos (id,user_id,path,moderation_state,created_at,updated_at)
		VALUES (?,?,?,?,?,?)`, "p1", u.ID, rel, "ok", now, now)
	require.NoError(t, err)

	code, _ := alex.json(http.MethodDelete, "/v1/me", nil)
	require.Equal(t, http.StatusOK, code)

	got, err := st.GetUser(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, "deleted", got.Status)
	require.NotNil(t, got.Name)
	require.Equal(t, "Deleted", *got.Name)
	require.False(t, fileExists(filepath.Join(s.PhotosDir, rel)), "photo file should be removed")

	// the session is invalidated
	code, _ = alex.json(http.MethodGet, "/v1/auth/session", nil)
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestDeleteMeRefusedUnderLegalHold(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	u := mustUser(t, st, "Alex")

	purge := store.FmtTS(time.Now().UTC().Add(90 * 24 * time.Hour))
	now := store.NowTS()
	_, err := st.DB.Exec(`INSERT INTO legal_holds (id,user_id,reason,status,purge_after,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?)`, uuid.NewString(), u.ID, "csam", "active", purge, now, now)
	require.NoError(t, err)

	code, body := alex.json(http.MethodDelete, "/v1/me", nil)
	require.Equal(t, http.StatusConflict, code)
	require.Equal(t, "legal_hold", body["error"].(map[string]any)["code"])
}

func TestForcePairRequiresRoleAndFlag(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	jordanID := mustUserID(t, st, "Jordan")

	// a normal user cannot force a pair
	code, _ := alex.json(http.MethodPost, "/v1/internal/force-pair", map[string]any{"otherUserId": jordanID, "gameKind": "this_or_that"})
	require.Equal(t, http.StatusNotFound, code)

	// an admin with the flag on can
	staff := login(t, s, "+15550000000")
	code, body := staff.json(http.MethodPost, "/v1/internal/force-pair", map[string]any{"otherUserId": jordanID, "gameKind": "this_or_that"})
	require.Equal(t, http.StatusOK, code)
	sessionID := body["sessionId"].(string)
	session, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, "invite", session.Mode)

	// with the flag off it is hidden again
	require.NoError(t, st.SetFlag(ctx, "staff.force_pair", false))
	code, _ = staff.json(http.MethodPost, "/v1/internal/force-pair", map[string]any{"otherUserId": jordanID})
	require.Equal(t, http.StatusNotFound, code)
}

func TestMeXpBadges(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	u := mustUser(t, st, "Alex")

	require.NoError(t, st.InsertXpEvent(ctx, u.ID, "session_complete", 50))
	require.NoError(t, st.InsertXpEvent(ctx, u.ID, "mutual", 20))
	require.NoError(t, st.InsertXpEvent(ctx, u.ID, "first_message", 10))
	require.NoError(t, st.SetUserXPLevel(ctx, u.ID, 100, 2))

	code, body := alex.json(http.MethodGet, "/v1/me/xp", nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, float64(100), body["xp"])
	require.Equal(t, float64(2), body["level"])

	ids := []string{}
	for _, b := range body["badges"].([]any) {
		ids = append(ids, b.(map[string]any)["id"].(string))
	}
	require.ElementsMatch(t, []string{"first-game", "spark", "icebreaker", "regular"}, ids)
	require.Len(t, body["events"].([]any), 3)
}

func TestSessionAnswerAfterCompletionIsRejected(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	// a terminal session must not accept answers
	sessionID := uuid.NewString()
	now := store.NowTS()
	_, err := st.DB.Exec(`INSERT INTO game_sessions (id,kind,mode,state,config,current_round,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, sessionID, "this_or_that", "queue_1v1", "completed", `{"rounds":8}`, 0, now, now)
	require.NoError(t, err)
	for i, uid := range []string{alexID, jordanID} {
		_, err = st.DB.Exec(`INSERT INTO session_participants (session_id,user_id,seat,joined_at,last_poll_at)
			VALUES (?,?,?,?,?)`, sessionID, uid, i, now, now)
		require.NoError(t, err)
	}

	alex := login(t, s, "+15551111111")
	code, _ := alex.json(http.MethodPost, "/v1/sessions/"+sessionID+"/answer", map[string]any{"payload": map[string]string{"choice": "left"}})
	require.Equal(t, http.StatusConflict, code)
	require.NotNil(t, ctx)
}
