package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// A CSAM or underage report hides the account. The reported user must not be
// able to clear that by driving the onboarding state machine, which is what
// happened: PATCH /v1/me took a client-supplied onboarding_step, set
// status='active' alongside it, and wrote the whole row back.
func TestReportedUserCannotUndoTheirOwnHide(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	victim := login(t, s, "+15553333333")
	victimID := mustUserIDByPhone(t, st, "+15553333333")
	reporter := login(t, s, "+15551111111")

	// Someone reports the victim for CSAM, which hides the account.
	code, body := reporter.json(http.MethodPost, "/v1/reports", map[string]any{
		"reason": "csam",
		"userId": victimID,
	})
	require.Equal(t, http.StatusOK, code, "report body: %v", body)

	var status string
	require.NoError(t, st.DB.GetContext(ctx, &status,
		`SELECT status FROM users WHERE id = ?`, victimID))
	require.Equal(t, "hidden", status, "a csam report must hide the account")

	// The hidden user tries to walk themselves back to active.
	code, body = victim.json(http.MethodPatch, "/v1/me", map[string]any{
		"onboarding_step": "done",
	})
	require.Equal(t, http.StatusOK, code, "patch body: %v", body)

	require.NoError(t, st.DB.GetContext(ctx, &status,
		`SELECT status FROM users WHERE id = ?`, victimID))
	require.Equal(t, "hidden", status,
		"a client-supplied onboarding_step must not restore a moderated account")
}

// The onboarding step itself should still be recorded: the user just must not
// gain the status change that comes with completing it.
func TestOnboardingStepStillSavesWhenHidden(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	victim := login(t, s, "+15553333333")
	victimID := mustUserIDByPhone(t, st, "+15553333333")
	reporter := login(t, s, "+15551111111")

	_, _ = reporter.json(http.MethodPost, "/v1/reports", map[string]any{
		"reason": "csam", "userId": victimID,
	})

	_, _ = victim.json(http.MethodPatch, "/v1/me", map[string]any{"onboarding_step": "done"})

	var step string
	require.NoError(t, st.DB.GetContext(ctx, &step,
		`SELECT onboarding_step FROM users WHERE id = ?`, victimID))
	require.Equal(t, "done", step)
}

// The ordinary path must be unaffected: finishing onboarding makes an active
// account active.
func TestFinishingOnboardingActivatesNormally(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	c := login(t, s, "+15553333333")
	id := mustUserIDByPhone(t, st, "+15553333333")

	_, err := st.DB.ExecContext(ctx, `UPDATE users SET status='pending' WHERE id = ?`, id)
	require.NoError(t, err)

	code, body := c.json(http.MethodPatch, "/v1/me", map[string]any{"onboarding_step": "done"})
	require.Equal(t, http.StatusOK, code, "patch body: %v", body)

	var status string
	require.NoError(t, st.DB.GetContext(ctx, &status, `SELECT status FROM users WHERE id = ?`, id))
	require.Equal(t, "active", status)
}
