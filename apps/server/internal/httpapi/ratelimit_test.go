package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"gamematch/internal/config"
	"gamematch/internal/sms"
	"gamematch/internal/turnstile"
)

// fakeTurnstile records calls and returns a scripted verdict.
type fakeTurnstile struct {
	ok    bool
	calls int
}

func (f *fakeTurnstile) Verify(_ context.Context, token, _ string) (bool, error) {
	f.calls++
	return f.ok && token != "", nil
}

func TestQueueRateLimited(t *testing.T) {
	s, _ := newTestServer(t)
	s.DisableRateLimits = false
	alex := login(t, s, "+15551111111")

	var code int
	for i := 0; i <= limitQueuePerUser; i++ {
		code, _ = alex.json(http.MethodPost, "/v1/queue", map[string]any{"gameKind": "this_or_that"})
	}
	require.Equal(t, http.StatusTooManyRequests, code)
}

func TestSessionPollRateLimited(t *testing.T) {
	s, st := newTestServer(t)
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	sessionID := insertSession(t, st, "this_or_that", "queue_1v1", "in_round", alexID, jordanID)

	s.DisableRateLimits = false
	alex := login(t, s, "+15551111111")

	codes := []int{}
	for i := 0; i < limitSessionPollPerUser+1; i++ {
		code, _ := alex.json(http.MethodGet, "/v1/sessions/"+sessionID, nil)
		codes = append(codes, code)
	}
	require.Equal(t, []int{200, 200, 429}, codes)
}

func TestReportsRateLimitedButCsamAlwaysAllowed(t *testing.T) {
	s, st := newTestServer(t)
	s.DisableRateLimits = false
	alex := login(t, s, "+15551111111")
	jordanID := mustUserID(t, st, "Jordan")

	for i := 0; i < limitReportsPerUser; i++ {
		code, _ := alex.json(http.MethodPost, "/v1/reports", map[string]any{"userId": jordanID, "reason": "spam"})
		require.Equal(t, http.StatusOK, code, "report %d", i+1)
	}
	code, _ := alex.json(http.MethodPost, "/v1/reports", map[string]any{"userId": jordanID, "reason": "spam"})
	require.Equal(t, http.StatusTooManyRequests, code)

	// Safety-critical reasons are never dropped (§Reports).
	code, _ = alex.json(http.MethodPost, "/v1/reports", map[string]any{"userId": jordanID, "reason": "csam"})
	require.Equal(t, http.StatusOK, code)
}

func TestMessagesRateLimitedPerThread(t *testing.T) {
	s, st := newTestServer(t)
	alexID := mustUserID(t, st, "Alex")
	jordanID := mustUserID(t, st, "Jordan")
	_, _, threadID := seedMutualPair(t, st, alexID, jordanID)

	s.DisableRateLimits = false
	alex := login(t, s, "+15551111111")

	var code int
	for i := 0; i <= limitMessagesPerThread; i++ {
		code, _ = alex.json(http.MethodPost, "/v1/threads/"+threadID+"/messages", map[string]any{"body": "hi"})
	}
	require.Equal(t, http.StatusTooManyRequests, code)
}

func TestTurnstileRequiredAfterRepeatedOTPFailures(t *testing.T) {
	s, _ := newTestServer(t)
	fake := &fakeTurnstile{}
	s.Turnstile = fake

	anon := newClient(t, s)
	phone := "+15551111111"

	for i := 0; i < limitOTPFailuresPerIP+1; i++ {
		code, _ := anon.json(http.MethodPost, "/v1/auth/otp/start", map[string]any{"phone": phone})
		require.Equal(t, http.StatusOK, code)
		code, _ = anon.json(http.MethodPost, "/v1/auth/otp/verify", map[string]any{"phone": phone, "code": "000000"})
		require.Equal(t, http.StatusUnauthorized, code)
	}

	code, body := anon.json(http.MethodPost, "/v1/auth/otp/start", map[string]any{"phone": phone})
	require.Equal(t, http.StatusForbidden, code)
	require.Equal(t, "captcha_required", body["error"].(map[string]any)["code"])

	// A passing challenge clears the block.
	fake.ok = true
	code, _ = anon.json(http.MethodPost, "/v1/auth/otp/start", map[string]any{"phone": phone, "turnstileToken": "token"})
	require.Equal(t, http.StatusOK, code)
	require.Positive(t, fake.calls)
}

func TestTurnstileNoopWhenUnconfigured(t *testing.T) {
	_, err := turnstile.Noop{}.Verify(context.Background(), "", "1.2.3.4")
	require.NoError(t, err)

	provider := sms.New(config.Config{})
	require.IsType(t, sms.LogProvider{}, provider)

	twilio := sms.New(config.Config{
		SMSProvider:     "twilio",
		TwilioAccountID: "AC123",
		TwilioAuthToken: "secret",
		TwilioFrom:      "+15550000000",
	})
	require.IsType(t, sms.TwilioProvider{}, twilio)
}

func TestOTPStartSilent(t *testing.T) {
	s, _ := newTestServer(t)
	s.Turnstile = &fakeTurnstile{ok: true}

	// A junk phone still answers {ok:true} and never reveals anything.
	code, body := newClient(t, s).json(http.MethodPost, "/v1/auth/otp/start", map[string]any{"phone": "nope"})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, body["ok"])
	require.NotContains(t, body, "devCode")
}
