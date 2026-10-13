package media

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// These are intimate photos on a dating app. At 0644 any local user, and any
// process sharing the host or a container volume, can read them. The systemd
// unit's ProtectSystem=strict does not change file modes.
func TestUploadedFilesAreNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteFile(filepath.Join(dir, "photo.jpg"), []byte("x")))

	info, err := os.Stat(filepath.Join(dir, "photo.jpg"))
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o004,
		"photo files must not be world-readable; mode was %v", info.Mode().Perm())
	require.Zero(t, info.Mode().Perm()&0o002,
		"photo files must not be world-writable; mode was %v", info.Mode().Perm())
}

func TestPhotoDirectoriesAreNotWorldTraversable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "photos")
	require.NoError(t, MkdirAll(dir))

	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o005,
		"photo directories must not be world-readable or traversable; mode was %v", info.Mode().Perm())
}
