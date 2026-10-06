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
