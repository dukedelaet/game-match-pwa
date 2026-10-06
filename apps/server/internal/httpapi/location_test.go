package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetLocationSnapsToTheNearestMetro(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	alex := login(t, s, "+15551111111")

	var laID string
	require.NoError(t, st.DB.Get(&laID, `SELECT id FROM metros WHERE slug = 'los-angeles'`))

	// A point near Los Angeles.
	code, body := alex.json(http.MethodPost, "/v1/me/location", map[string]any{"lat": 34.10, "lng": -118.30})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, laID, body["metroId"])
	require.Equal(t, "Los Angeles", body["cityLabel"])
	require.NotContains(t, body, "lat")
	require.NotContains(t, body, "lng")

	// The database holds the coarse hash and the source, never the coordinate.
	var stored struct {
		MetroID *string `db:"metro_id"`
		Geohash *string `db:"approx_geohash"`
		Source  *string `db:"approx_geohash_source"`
	}
	require.NoError(t, st.DB.GetContext(ctx, &stored, `
		SELECT metro_id, approx_geohash, approx_geohash_source FROM users WHERE id = ?`,
		mustUserID(t, st, "Alex")))
	require.NotNil(t, stored.MetroID)
	require.Equal(t, laID, *stored.MetroID)
	require.NotNil(t, stored.Geohash)
	require.Len(t, *stored.Geohash, 5, "only precision-5 geohashes are stored")
	require.NotNil(t, stored.Source)
	require.Equal(t, "geo", *stored.Source)
}

func TestSetLocationRequiresCoordinates(t *testing.T) {
	s, _ := newTestServer(t)
	alex := login(t, s, "+15551111111")

	code, _ := alex.json(http.MethodPost, "/v1/me/location", map[string]any{"lat": 34.1})
	require.Equal(t, http.StatusUnprocessableEntity, code)
}

func TestSetLocationRequiresAuth(t *testing.T) {
	s, _ := newTestServer(t)
	code, _ := newClient(t, s).json(http.MethodPost, "/v1/me/location", map[string]any{"lat": 1, "lng": 2})
	require.Equal(t, http.StatusUnauthorized, code)
}
