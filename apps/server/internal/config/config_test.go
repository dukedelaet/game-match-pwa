package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("ADDR", "0.0.0.0:9999")
	t.Setenv("DATA_DIR", "/tmp/gm-data")
	t.Setenv("APP_ENV", "prod")
	t.Setenv("APP_DEBUG", "true")
	t.Setenv("HOUSE_USER_ID", "house-1")
	t.Setenv("DEV_OTP", "000000")

	cfg := Load()
	require.Equal(t, "0.0.0.0:9999", cfg.Addr)
	require.Equal(t, "/tmp/gm-data", cfg.DataDir)
	require.Equal(t, "prod", cfg.Env)
	require.True(t, cfg.Debug)
	require.Equal(t, "house-1", cfg.HouseUserID)
	require.Equal(t, "000000", cfg.DevOTP)
	require.Equal(t, filepath.Join("/tmp/gm-data", "gamematch.db"), cfg.DBPath())
	require.Equal(t, filepath.Join("/tmp/gm-data", "photos"), cfg.PhotosDir())
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("ADDR", "")
	t.Setenv("PORT", "")
	t.Setenv("DATA_DIR", "/tmp/gm-default")
	t.Setenv("APP_ENV", "")
	t.Setenv("APP_DEBUG", "")
	t.Setenv("HOUSE_USER_ID", "")
	t.Setenv("DEV_OTP", "")

	cfg := Load()
	require.Equal(t, "127.0.0.1:8000", cfg.Addr)
	require.Equal(t, "local", cfg.Env)
	require.False(t, cfg.Debug)
	require.Equal(t, "00000000-0000-4000-8000-000000000001", cfg.HouseUserID)
	require.Equal(t, "123456", cfg.DevOTP)
}

func TestLoadDerivesDataDirWhenUnset(t *testing.T) {
	t.Setenv("DATA_DIR", "")
	cfg := Load()
	require.NotEmpty(t, cfg.DataDir)
}

func prodOK() Config {
	return Config{
		Env:             "production",
		SMSProvider:     "twilio",
		TwilioAccountID: "AC1",
		TwilioAuthToken: "tok",
		TwilioFrom:      "+15550000000",
		TurnstileSecret: "0xsecret",
	}
}

func TestValidateRejectsUnsafeProduction(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"debug enabled", func(c *Config) { c.Debug = true }, "APP_DEBUG"},
		{"dev otp set", func(c *Config) { c.DevOTP = "123456" }, "DEV_OTP"},
		{"log sms provider", func(c *Config) { c.SMSProvider = "log" }, "SMS_PROVIDER"},
		{"twilio without auth token", func(c *Config) { c.TwilioAuthToken = "" }, "TWILIO_AUTH_TOKEN"},
		{"twilio without from", func(c *Config) { c.TwilioFrom = "" }, "TWILIO_FROM"},
		{"turnstile missing", func(c *Config) { c.TurnstileSecret = "" }, "TURNSTILE_SECRET"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := prodOK()
			tt.mutate(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestValidateAcceptsSafeProduction(t *testing.T) {
	require.NoError(t, prodOK().Validate())
}

func TestValidateAllowsInsecureSettingsOutsideProduction(t *testing.T) {
	for _, env := range []string{"local", "test", "staging", ""} {
		cfg := Config{Env: env, Debug: true, DevOTP: "123456", SMSProvider: "log"}
		require.NoErrorf(t, cfg.Validate(), "env=%q", env)
	}
}

func TestValidateRejectsUnknownEnv(t *testing.T) {
	cfg := prodOK()
	cfg.Env = "prduction"
	require.Error(t, cfg.Validate())
}
