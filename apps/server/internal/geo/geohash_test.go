package geo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeLengthAndKnownPrefix(t *testing.T) {
	hash := Encode(34.05, -118.24, 5)
	require.Len(t, hash, 5)
	// Los Angeles sits in the 9q cell.
	require.Equal(t, "9q", hash[:2])
}

func TestEncodeNearbyPointsShareAPrefix(t *testing.T) {
	a := Encode(34.0500, -118.2400, 6)
	b := Encode(34.0510, -118.2410, 6)
	require.Equal(t, a[:4], b[:4], "points a block apart share a coarse cell")

	far := Encode(40.71, -74.00, 6)
	require.NotEqual(t, a[:2], far[:2], "LA and NYC are in different cells")
}

func TestNearestPicksTheClosestPoint(t *testing.T) {
	points := []Point{
		{ID: "la", Label: "Los Angeles", Lat: 34.05, Lng: -118.24},
		{ID: "ny", Label: "New York", Lat: 40.71, Lng: -74.00},
	}

	near, ok := Nearest(34.10, -118.30, points)
	require.True(t, ok)
	require.Equal(t, "la", near.ID)

	near, ok = Nearest(40.80, -73.90, points)
	require.True(t, ok)
	require.Equal(t, "ny", near.ID)
}

func TestNearestWithNoCandidates(t *testing.T) {
	_, ok := Nearest(0, 0, nil)
	require.False(t, ok)
}
