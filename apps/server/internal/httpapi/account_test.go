package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

func TestMatchesListsMutualWithPairAndThread(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	pairID, matchID, threadID := seedMutualPair(t, st, alexID, jordanID)

	code, body := alex.json(http.MethodGet, "/v1/matches", nil)
	require.Equal(t, http.StatusOK, code)
	items := body["items"].([]any)
	require.Len(t, items, 1)

	item := items[0].(map[string]any)
	require.Equal(t, matchID, item["id"])
	require.Equal(t, "mutual", item["state"])
	require.Equal(t, pairID, item["pairId"])
	require.Equal(t, threadID, item["threadId"])
	require.Equal(t, jordanID, item["other"].(map[string]any)["id"])
}

func TestMatchesHidesBlocked(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	seedMutualPair(t, st, alexID, jordanID)

	code, _ := alex.json(http.MethodPost, "/v1/blocks", map[string]any{"userId": jordanID})
	require.Equal(t, http.StatusOK, code)

	_, body := alex.json(http.MethodGet, "/v1/matches", nil)
	require.Empty(t, body["items"].([]any))
}

func TestMeBlocksListsDirectedBlocks(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	jordanID := mustUserID(t, st, "Jordan")

	code, _ := alex.json(http.MethodPost, "/v1/blocks", map[string]any{"userId": jordanID})
	require.Equal(t, http.StatusOK, code)

	code, body := alex.json(http.MethodGet, "/v1/me/blocks", nil)
	require.Equal(t, http.StatusOK, code)
	items := body["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, jordanID, items[0].(map[string]any)["userId"])
}

func TestExportOmitsSecrets(t *testing.T) {
	s, st := newTestServer(t)
	alex := login(t, s, "+15551111111")
	u := mustUser(t, st, "Alex")

	// Give the user data that must not leak.
	_, err := st.DB.Exec(`UPDATE users SET dob='1998-01-15', approx_geohash='9q8yy', email='alex@example.com' WHERE id=?`, u.ID)
	require.NoError(t, err)

	rec := alex.req(http.MethodGet, "/v1/me/export", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	text := rec.Body.String()
	for _, forbidden := range []string{"dob", "phone", "geohash", "email", "1998-01-15", "alex@example.com", "9q8yy"} {
		require.NotContains(t, text, forbidden, "export must not contain %q", forbidden)
	}
	require.Contains(t, text, "exportedAt")
	require.Contains(t, text, "Alex")
}

func TestBlockCancelsPendingInvites(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	insertPair(t, st, alexID, jordanID, "open_play", nil)

	_, invite := alex.json(http.MethodPost, "/v1/invites", map[string]any{"targetUserId": jordanID, "gameKind": "this_or_that"})
	inviteID := invite["id"].(string)

	code, _ := alex.json(http.MethodPost, "/v1/blocks", map[string]any{"userId": jordanID})
	require.Equal(t, http.StatusOK, code)

	inv, err := st.GetInvite(ctx, inviteID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", inv.State)
}

func TestMatchExpiryExpiresPairAndLocksThread(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	pairID, matchID, threadID := seedMutualPair(t, st, alexID, jordanID)
	_, err := st.DB.Exec(`UPDATE matches SET expires_at=? WHERE id=?`,
		store.FmtTS(time.Now().UTC().Add(-time.Minute)), matchID)
	require.NoError(t, err)

	require.NoError(t, st.ExpireStaleMatches(ctx))

	m, err := st.MatchByID(ctx, matchID)
	require.NoError(t, err)
	require.Equal(t, "expired", m.State)

	p, err := st.PairByID(ctx, pairID)
	require.NoError(t, err)
	require.Equal(t, "expired", p.State)
	require.Nil(t, p.CooldownUntil, "expired pairs may re-queue immediately")

	th, err := st.ThreadByID(ctx, threadID)
	require.NoError(t, err)
	require.Equal(t, "locked", th.State)
}

func TestMatchExpiryLeavesFreshMatchesAlone(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	_, matchID, _ := seedMutualPair(t, st, alexID, jordanID)
	_, err := st.DB.Exec(`UPDATE matches SET expires_at=? WHERE id=?`,
		store.FmtTS(time.Now().UTC().Add(time.Hour)), matchID)
	require.NoError(t, err)

	require.NoError(t, st.ExpireStaleMatches(ctx))

	m, err := st.MatchByID(ctx, matchID)
	require.NoError(t, err)
	require.Equal(t, "mutual", m.State)
	require.False(t, strings.Contains(m.State, "expired"))
}
