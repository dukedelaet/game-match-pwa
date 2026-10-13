package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A freshly migrated database has no seed run against it. Sign-in is gated on
// feature flags that default to false when the row is missing, so the flags
// must be seeded by the migration itself.
func TestMigrateSeedsAuthFlags(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, Migrate(context.Background(), conn))

	for _, key := range []string{"auth.public_signup", "auth.otp"} {
		var value string
		err := conn.GetContext(context.Background(), &value,
			`SELECT value FROM feature_flags WHERE "key" = ?`, key)
		require.NoErrorf(t, err, "flag %q must exist after migration", key)
		require.JSONEqf(t, `{"v": true}`, value, "flag %q must default to enabled", key)
	}
}
