package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The limiter's bucket key is the table's PRIMARY KEY and nothing ever deleted
// a row, so the table was a permanent, plaintext index of every phone number
// that ever requested a code -- directly against the app's own "approximate
// city only, never exact location" privacy posture.
func TestRateLimitBucketsDoNotStoreRawPhoneNumbers(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	c := newClient(t, s)
	const phone = "+15551234567"
	// The limiter has to actually run for it to write a bucket key.
	s.DisableRateLimits = false

	code, body := c.json(http.MethodPost, "/v1/auth/otp/start", map[string]string{"phone": phone})
	require.Equal(t, http.StatusOK, code, "start body: %v", body)

	var buckets []string
	rows, err := st.DB.QueryContext(ctx, `SELECT bucket FROM rate_limits`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var b string
		require.NoError(t, rows.Scan(&b))
		buckets = append(buckets, b)
	}
	require.NoError(t, rows.Err())

	require.NotEmpty(t, buckets, "the limiter should have recorded the attempt")
	for _, b := range buckets {
		require.NotContains(t, b, phone, "bucket %q leaks the phone number", b)
		require.NotContains(t, b, "15551234567", "bucket %q leaks the phone number", b)
	}
}

// An IP is personal data too. Buckets keyed by it must be one-way.
func TestRateLimitBucketsDoNotStoreRawIPs(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	c := newClient(t, s)
	s.DisableRateLimits = false

	rec := c.reqWithHeader(http.MethodPost, "/v1/auth/otp/start",
		map[string]string{"phone": "+15559999999"}, "X-Forwarded-For", "203.0.113.42")
	require.Equal(t, http.StatusOK, rec.Code)

	var leaked int
	require.NoError(t, st.DB.QueryRowxContext(ctx,
		`SELECT count(*) FROM rate_limits WHERE bucket LIKE '%203.0.113.42%'`).Scan(&leaked))
	require.Zero(t, leaked, "bucket keys must not contain raw IP addresses")
}

// Expired rows must be swept: the table had no delete path at all, so even
// hashed identifiers accumulated forever.
func TestExpiredRateLimitRowsAreSwept(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	c := newClient(t, s)
	s.DisableRateLimits = false
	_, _ = c.json(http.MethodPost, "/v1/auth/otp/start", map[string]string{"phone": "+15551230001"})

	// Age one row far past the longest window.
	_, err := st.DB.ExecContext(ctx,
		`UPDATE rate_limits SET window_start = window_start - ?`,
		int64(48*60*60*1000))
	require.NoError(t, err)

	var before int
	require.NoError(t, st.DB.GetContext(ctx, &before, `SELECT count(*) FROM rate_limits`))
	require.Positive(t, before)

	require.NoError(t, s.Store.SweepRateLimits(ctx, 24))

	var after int
	require.NoError(t, st.DB.GetContext(ctx, &after, `SELECT count(*) FROM rate_limits`))
	require.Zero(t, after, "expired rows must be removed")
}

func TestSweepKeepsRowsInsideTheWindow(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()

	c := newClient(t, s)
	s.DisableRateLimits = false
	_, _ = c.json(http.MethodPost, "/v1/auth/otp/start", map[string]string{"phone": "+15551230002"})

	require.NoError(t, s.Store.SweepRateLimits(ctx, 24))

	var after int
	require.NoError(t, st.DB.GetContext(ctx, &after, `SELECT count(*) FROM rate_limits`))
	require.Positive(t, after, "rows inside the retention window must survive")
}
