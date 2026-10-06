package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

func TestRandomCodeIsSixDigits(t *testing.T) {
	for i := 0; i < 25; i++ {
		code := randomCode()
		require.Len(t, code, 6)
		for _, r := range code {
			require.True(t, r >= '0' && r <= '9', "code %q not numeric", code)
		}
	}
}

func firstID(t *testing.T, st *store.Store, query string) string {
	t.Helper()
	var id string
	require.NoError(t, st.DB.Get(&id, query))
	return id
}

func TestPatchMeSetsTraitsAndWhoToMeet(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")

	traitID := firstID(t, st, `SELECT id FROM traits ORDER BY sort LIMIT 1`)
	genderID := firstID(t, st, `SELECT id FROM genders ORDER BY sort LIMIT 1`)

	code, body := alex.json(http.MethodPatch, "/v1/me", map[string]any{
		"trait_ids":        []string{traitID},
		"who_to_meet":      []string{genderID},
		"who_to_meet_open": false,
		"age_min":          25,
		"age_max":          35,
		"distance_scope":   "metro_and_adjacent",
	})
	require.Equal(t, http.StatusOK, code)

	traits, err := st.TraitIDsFor(ctx, alexID)
	require.NoError(t, err)
	require.Equal(t, []string{traitID}, traits)

	prefs, err := st.PreferenceFor(ctx, alexID)
	require.NoError(t, err)
	require.False(t, prefs.WhoToMeetOpen)
	require.Equal(t, 25, prefs.AgeMin)
	require.Equal(t, 35, prefs.AgeMax)
	require.Equal(t, "metro_and_adjacent", prefs.DistanceScope)

	// the JSON array round-trips through the DTO
	dtoPrefs := body["user"].(map[string]any)["preferences"].(map[string]any)
	require.Equal(t, []any{genderID}, dtoPrefs["who_to_meet"])
	require.Equal(t, false, dtoPrefs["who_to_meet_open"])
}

func TestRequestLoggingMiddlewareRunsOutsideTests(t *testing.T) {
	s, _ := newTestServer(t)
	s.Cfg.Env = "local" // logging is enabled for non-test environments
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/healthz", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestChatIcebreakerKindIsNormalizedToText(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	_, _, threadID := seedMutualPair(t, st, alexID, jordanID)

	// an allowed icebreaker body is accepted
	code, _ := alex.json(http.MethodPost, "/v1/threads/"+threadID+"/messages", map[string]any{"body": "You pick", "kind": "icebreaker"})
	require.Equal(t, http.StatusOK, code)

	// an arbitrary body sent as an icebreaker is downgraded to plain text
	code, _ = alex.json(http.MethodPost, "/v1/threads/"+threadID+"/messages", map[string]any{"body": "something custom", "kind": "icebreaker"})
	require.Equal(t, http.StatusOK, code)

	msgs, err := st.MessagesForThread(ctx, threadID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	for _, m := range msgs {
		require.Equal(t, "text", m.Kind)
	}
}

func TestStaffAllowlistAddAndGatedSignup(t *testing.T) {
	s, _ := newTestServer(t)
	staff := login(t, s, "+15550000000")

	code, body := staff.json(http.MethodPost, "/v1/staff/allowlist", map[string]any{"phone": "+15557777777", "public_signup": false})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, false, body["publicSignup"])
	require.Equal(t, float64(4), body["count"], "3 seeded + the new one")

	anon := newClient(t, s)

	// signup is closed, so an unknown phone gets no code
	code, resp := anon.json(http.MethodPost, "/v1/auth/otp/start", map[string]any{"phone": "+15558888888"})
	require.Equal(t, http.StatusOK, code)
	require.NotContains(t, resp, "devCode")

	// an allowlisted phone does get a code
	code, resp = anon.json(http.MethodPost, "/v1/auth/otp/start", map[string]any{"phone": "+15557777777"})
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, resp, "devCode")
}

func TestRematchReopensExistingThread(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")

	pairID, _, threadID := seedMutualPair(t, st, alexID, jordanID)

	// put the pair back into a connectable state (clearing the cooldown)
	_, err := st.DB.Exec(`UPDATE pair_relationships SET state='open_play', a_action='none', b_action='none', cooldown_until=NULL WHERE id=?`, pairID)
	require.NoError(t, err)
	// lock the existing thread so we can observe the reopen
	require.NoError(t, st.PutThreadState(ctx, threadID, "locked"))

	jordan := login(t, s, "+15552222222")
	code, _ := alex.json(http.MethodPost, "/v1/pairs/"+pairID+"/connect", map[string]any{"action": "connect"})
	require.Equal(t, http.StatusOK, code)
	code, _ = jordan.json(http.MethodPost, "/v1/pairs/"+pairID+"/connect", map[string]any{"action": "connect"})
	require.Equal(t, http.StatusOK, code)

	// the same thread row is reopened, not recreated
	th, err := st.ThreadByID(ctx, threadID)
	require.NoError(t, err)
	require.Equal(t, "open", th.State)

	msgs, err := st.MessagesForThread(ctx, threadID)
	require.NoError(t, err)
	kinds := map[string]int{}
	for _, m := range msgs {
		kinds[m.Kind]++
	}
	require.GreaterOrEqual(t, kinds["system"], 1, "a 'You matched again' divider is appended")
	require.GreaterOrEqual(t, kinds["icebreaker"], 1)
}
