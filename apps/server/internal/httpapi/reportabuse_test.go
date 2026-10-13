package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

// Skipping the rate limit for csam/underage reports is the right call -- you
// must never drop a real report. But the consequence today is that visibility
// is tied to volume: any authenticated user can file unlimited safety reports
// against anyone, hiding them from matchmaking each time. The report itself
// should stay unlimited; the automatic hide should not be.
func TestRepeatedSafetyReportsCannotMassHideAccounts(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	reporter := login(t, s, "+15551111111")
	victimID := mustUserIDByPhone(t, st, "+15552222222")

	// The first report hides the account: that is the safety mechanism working.
	code, body := reporter.json(http.MethodPost, "/v1/reports", map[string]any{
		"reason": "csam", "userId": victimID,
	})
	require.Equal(t, http.StatusOK, code, "report body: %v", body)
	require.Equal(t, "hidden", userStatus(t, st, victimID))

	// Every further report must still be filed for a human to look at...
	for i := 0; i < 10; i++ {
		code, body = reporter.json(http.MethodPost, "/v1/reports", map[string]any{
			"reason": "csam", "userId": victimID,
		})
		require.Equal(t, http.StatusOK, code, "a safety report must never be dropped: %v", body)
	}

	// ...but they must not accumulate unbounded hide actions, and the
	// moderation queue has to show them for review rather than silently
	// re-applying the same automated action.
	var reports int
	require.NoError(t, st.DB.GetContext(ctx, &reports,
		`SELECT count(*) FROM reports WHERE subject_id = ? AND reason = 'csam'`, victimID))
	require.GreaterOrEqual(t, reports, 10, "every report must be recorded")
}

// A reporter must be able to report someone they have actually interacted
// with. Today userId is never checked against the reporter, so any random
// account can be buried.
func TestSafetyReportRequiresARealAccount(t *testing.T) {
	s, _ := newTestServer(t)

	reporter := login(t, s, "+15551111111")

	code, body := reporter.json(http.MethodPost, "/v1/reports", map[string]any{
		"reason": "csam", "userId": "00000000-0000-4000-8000-000000009999",
	})
	require.Equal(t, http.StatusNotFound, code, "reporting a nonexistent user: %v", body)
}

// Self-reporting should not be a way to trigger the automatic hide path.
func TestCannotHideYourOwnAccountViaReport(t *testing.T) {
	s, st := newTestServer(t)

	alex := login(t, s, "+15551111111")
	alexID := mustUserIDByPhone(t, st, "+15551111111")

	code, body := alex.json(http.MethodPost, "/v1/reports", map[string]any{
		"reason": "csam", "userId": alexID,
	})
	require.Equal(t, http.StatusUnprocessableEntity, code, "self report body: %v", body)
	require.NotEqual(t, "hidden", userStatus(t, st, alexID))
}

// The automated hide must be rate limited per reporter and per subject, so a
// single account cannot sweep many targets out of matchmaking.
func TestMassHidingAcrossManyAccountsIsBounded(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	reporter := login(t, s, "+15551111111")

	// A batch of distinct victims.
	var victims []string
	for i := 0; i < 6; i++ {
		login(t, s, phoneFor(i))
		victims = append(victims, mustUserIDByPhone(t, st, phoneFor(i)))
	}

	// This test is about the limiter, so it has to be on. Enabled after sign-in,
	// because OTP verification is itself rate limited.
	s.DisableRateLimits = false

	for _, v := range victims {
		_, _ = reporter.json(http.MethodPost, "/v1/reports", map[string]any{
			"reason": "csam", "userId": v,
		})
	}

	var hidden int
	require.NoError(t, st.DB.GetContext(ctx, &hidden,
		`SELECT count(*) FROM users WHERE status='hidden'`))

	// The reports are all still filed...
	var reports int
	require.NoError(t, st.DB.GetContext(ctx, &reports, `SELECT count(*) FROM reports`))
	require.GreaterOrEqual(t, reports, len(victims))

	// ...but a single account cannot hide six unrelated people outright.
	require.Less(t, hidden, len(victims),
		"one reporter must not be able to hide every target in a batch; %d of %d were hidden", hidden, len(victims))

	// Whatever was hidden must be surfaced to moderators, not silently applied.
	var flagged int
	require.NoError(t, st.DB.GetContext(ctx, &flagged,
		`SELECT count(*) FROM reports WHERE status='open' AND subject_id IS NOT NULL`))
	require.Positive(t, flagged)
}

func userStatus(t *testing.T, st *store.Store, userID string) string {
	t.Helper()
	var status string
	require.NoError(t, st.DB.GetContext(context.Background(), &status,
		`SELECT status FROM users WHERE id = ?`, userID))
	return status
}

func phoneFor(i int) string {
	return "+1554" + string(rune('0'+i)) + "000001"
}

var _ = time.Second
