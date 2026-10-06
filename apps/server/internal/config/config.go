package config

import (
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
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Load reads configuration from environment variables, applying defaults.
func Load() Config {
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
	return Config{
		Addr:        addr,
		DataDir:     dataDir,
		Env:         getenv("APP_ENV", "local"),
		Debug:       debug,
		HouseUserID: getenv("HOUSE_USER_ID", "00000000-0000-4000-8000-000000000001"),
		DevOTP:      getenv("DEV_OTP", "123456"),
	}
}

// DBPath is the SQLite file path.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "gamematch.db") }

// PhotosDir is where uploaded photos live.
func (c Config) PhotosDir() string { return filepath.Join(c.DataDir, "photos") }
