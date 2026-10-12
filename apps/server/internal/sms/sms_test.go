package sms

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gamematch/internal/config"
)

// captureLog redirects the standard logger into a buffer for one test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(log.LstdFlags) })
	return &buf
}

func TestLogProviderNeverLogsTheCode(t *testing.T) {
	buf := captureLog(t)
	require.NoError(t, LogProvider{}.Send(context.Background(), "+15551234567", "Your GameMatch code is 123456"))
	require.NotContains(t, buf.String(), "123456")
	require.Contains(t, buf.String(), "**********67")
}

func TestNewCheckedFailsInProductionWithoutTwilio(t *testing.T) {
	_, err := NewChecked(config.Config{Env: "production", SMSProvider: "log"})
	require.Error(t, err)
}

func TestNewCheckedFailsInProductionWithPartialTwilioCreds(t *testing.T) {
	_, err := NewChecked(config.Config{
		Env:             "production",
		SMSProvider:     "twilio",
		TwilioAccountID: "AC1",
	})
	require.Error(t, err)
}

func TestNewCheckedReturnsTwilioInProduction(t *testing.T) {
	p, err := NewChecked(config.Config{
		Env:             "production",
		SMSProvider:     "twilio",
		TwilioAccountID: "AC1",
		TwilioAuthToken: "tok",
		TwilioFrom:      "+15550000000",
	})
	require.NoError(t, err)
	require.IsType(t, TwilioProvider{}, p)
}

func TestNewCheckedFallsBackToLogOutsideProduction(t *testing.T) {
	p, err := NewChecked(config.Config{Env: "local"})
	require.NoError(t, err)
	require.IsType(t, LogProvider{}, p)
}

func TestFatalLogProviderAlwaysErrors(t *testing.T) {
	captureLog(t)
	err := (FatalLogProvider{}).Send(context.Background(), "+15551234567", "code 123456")
	require.ErrorIs(t, err, ErrNoProvider)
}

func TestMaskPhone(t *testing.T) {
	require.Equal(t, "**", maskPhone("12"))
	require.Equal(t, "**********67", maskPhone("+15551234567"))
	require.True(t, strings.HasSuffix(maskPhone("+15551234567"), "67"))
}
