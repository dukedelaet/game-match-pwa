package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the runtime configuration, loaded from the environment.
type Config struct {
	Addr        string
	DataDir     string
	Env         string
	Debug       bool
	HouseUserID string
	DevOTP      string

	// Optional integrations. Empty means "not configured": the code path
	// degrades to a logged no-op instead of failing.
	SMSProvider     string // "log" (default) or "twilio"
	TwilioAccountID string
	TwilioAuthToken string
	TwilioFrom      string

	TurnstileSecret string

	GoogleClientID  string
	AppleClientID   string
	AppleTeamID     string
	AppleKeyID      string
	ApplePrivateKey string

	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Load reads configuration from an optional dotenv file and then the
// environment (environment wins), applying defaults.
func Load() Config {
	if path := getenv("ENV_FILE", ""); path != "" {
		_ = LoadDotenv(path)
	} else {
		_ = LoadDotenv(".env")
	}

	dataDir := getenv("DATA_DIR", "")
	if dataDir == "" {
		// Default to <repo>/apps/server/data, or ./data when cwd is the server dir.
		if wd, err := os.Getwd(); err == nil {
			if strings.HasSuffix(wd, filepath.Join("apps", "server")) {
				dataDir = filepath.Join(wd, "data")
			} else if strings.HasSuffix(wd, "cmd"+string(filepath.Separator)+"gamematch") {
				dataDir = filepath.Join(wd, "..", "..", "data")
			} else {
				dataDir = filepath.Join(wd, "apps", "server", "data")
			}
		} else {
			dataDir = "data"
		}
	}
	addr := getenv("ADDR", "")
	if addr == "" {
		addr = "127.0.0.1:" + getenv("PORT", "8000")
	}
	debug := false
	if v, err := strconv.ParseBool(getenv("APP_DEBUG", "false")); err == nil {
		debug = v
	}
	env := getenv("APP_ENV", "local")
	return Config{
		Addr:            addr,
		DataDir:         dataDir,
		Env:             env,
		Debug:           debug,
		HouseUserID:     getenv("HOUSE_USER_ID", "00000000-0000-4000-8000-000000000001"),
		DevOTP:          getenv("DEV_OTP", devOTPDefault(env)),
		SMSProvider:     getenv("SMS_PROVIDER", "log"),
		TwilioAccountID: getenv("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken: getenv("TWILIO_AUTH_TOKEN", ""),
		TwilioFrom:      getenv("TWILIO_FROM", ""),
		TurnstileSecret: getenv("TURNSTILE_SECRET", ""),
		GoogleClientID:  getenv("GOOGLE_CLIENT_ID", ""),
		AppleClientID:   getenv("APPLE_CLIENT_ID", ""),
		AppleTeamID:     getenv("APPLE_TEAM_ID", ""),
		AppleKeyID:      getenv("APPLE_KEY_ID", ""),
		ApplePrivateKey: getenv("APPLE_PRIVATE_KEY", ""),
		VAPIDPublicKey:  getenv("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey: getenv("VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:    getenv("VAPID_SUBJECT", "mailto:ops@example.com"),
	}
}

// IsProduction reports whether this process is serving live traffic.
func (c Config) IsProduction() bool {
	return c.Env == "production" || c.Env == "prod"
}

// Validate rejects configurations that are unsafe on live traffic. It is
// called once at boot; main fatals on a non-nil return.
//
// Outside production every integration may be left unset so local and test
// runs work with no secrets at all.
func (c Config) Validate() error {
	switch c.Env {
	case "local", "test", "staging", "production", "prod", "":
	default:
		return fmt.Errorf("APP_ENV: %q is not one of local, test, staging, production", c.Env)
	}
	if !c.IsProduction() {
		return nil
	}
	var missing []string
	if c.Debug {
		missing = append(missing, "APP_DEBUG (must be false; it returns OTPs in the response body)")
	}
	if c.DevOTP != "" {
		missing = append(missing, "DEV_OTP (must be unset; it replaces the random OTP with a constant)")
	}
	if c.SMSProvider != "twilio" {
		missing = append(missing, "SMS_PROVIDER (must be \"twilio\"; \"log\" writes OTPs to the journal in cleartext)")
	}
	for _, v := range []struct{ name, val string }{
		{"TWILIO_ACCOUNT_SID", c.TwilioAccountID},
		{"TWILIO_AUTH_TOKEN", c.TwilioAuthToken},
		{"TWILIO_FROM", c.TwilioFrom},
		{"TURNSTILE_SECRET", c.TurnstileSecret},
	} {
		if v.val == "" {
			missing = append(missing, v.name+" (required in production)")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("unsafe production configuration: %s", strings.Join(missing, "; "))
	}
	return nil
}

// DBPath is the SQLite file path.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "gamematch.db") }

// PhotosDir is where uploaded photos live.
func (c Config) PhotosDir() string { return filepath.Join(c.DataDir, "photos") }

// devOTPDefault is the fixed OTP used for local and test sign-in. It is empty
// in production, so the well-known demo code cannot reach a live deployment
// even when DEV_OTP is unset there.
func devOTPDefault(env string) string {
	if env == "production" || env == "prod" {
		return ""
	}
	return "123456"
}
