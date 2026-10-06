package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var dotenvKeys = []string{"GM_TEST_PLAIN", "GM_TEST_QUOTED", "GM_TEST_SINGLE", "GM_TEST_EXPORT", "GM_TEST_KEEP"}

func clearDotenvKeys(t *testing.T) {
	t.Helper()
	for _, k := range dotenvKeys {
		_ = os.Unsetenv(k)
	}
	t.Cleanup(func() {
		for _, k := range dotenvKeys {
			_ = os.Unsetenv(k)
		}
	})
}

func TestLoadDotenvParsesValuesAndQuotes(t *testing.T) {
	clearDotenvKeys(t)

	path := filepath.Join(t.TempDir(), ".env")
	content := "# a comment\n" +
		"\n" +
		"GM_TEST_PLAIN=value\n" +
		"GM_TEST_QUOTED=\"quoted value\"\n" +
		"GM_TEST_SINGLE='single value'\n" +
		"export GM_TEST_EXPORT=exported\n" +
		"NOT_A_PAIR\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	require.NoError(t, LoadDotenv(path))

	require.Equal(t, "value", os.Getenv("GM_TEST_PLAIN"))
	require.Equal(t, "quoted value", os.Getenv("GM_TEST_QUOTED"))
	require.Equal(t, "single value", os.Getenv("GM_TEST_SINGLE"))
	require.Equal(t, "exported", os.Getenv("GM_TEST_EXPORT"))
}

func TestLoadDotenvMissingFileIsNotAnError(t *testing.T) {
	require.NoError(t, LoadDotenv(filepath.Join(t.TempDir(), "absent.env")))
}

func TestLoadDotenvDoesNotOverrideTheEnvironment(t *testing.T) {
	clearDotenvKeys(t)
	t.Setenv("GM_TEST_KEEP", "from-env")

	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte("GM_TEST_KEEP=from-file\n"), 0o600))

	require.NoError(t, LoadDotenv(path))
	require.Equal(t, "from-env", os.Getenv("GM_TEST_KEEP"))
}

func TestLoadReadsConfigFromEnvFile(t *testing.T) {
	// Use unique values that would otherwise fall back to defaults.
	for _, k := range []string{"ADDR", "DATA_DIR", "APP_DEBUG", "APP_ENV", "HOUSE_USER_ID", "DEV_OTP"} {
		_ = os.Unsetenv(k)
	}
	path := filepath.Join(t.TempDir(), "gamematch.env")
	content := "ADDR=127.0.0.1:1234\nDATA_DIR=/tmp/gm-from-file\nAPP_DEBUG=true\nAPP_ENV=prod\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	t.Setenv("ENV_FILE", path)
	t.Cleanup(func() {
		for _, k := range []string{"ADDR", "DATA_DIR", "APP_DEBUG", "APP_ENV"} {
			_ = os.Unsetenv(k)
		}
	})

	cfg := Load()
	require.Equal(t, "127.0.0.1:1234", cfg.Addr)
	require.Equal(t, "/tmp/gm-from-file", cfg.DataDir)
	require.True(t, cfg.Debug)
	require.Equal(t, "prod", cfg.Env)
}
