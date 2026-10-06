// Package geo provides the small geohash helpers the app needs. Raw
// coordinates are never persisted or logged; only the coarse hash is.
package geo

import "math"

const base32 = "0123456789bcdefghjkmnpqrstuvwxyz"

// Encode returns the geohash of a point at the given precision. Precision 5 is
// roughly 4.9km, which is the only precision the app stores.
func Encode(lat, lng float64, precision int) string {
	latRange := [2]float64{-90, 90}
	lngRange := [2]float64{-180, 180}

	var hash []byte
	bit, ch, even := 0, 0, true
	for len(hash) < precision {
		if even {
			mid := (lngRange[0] + lngRange[1]) / 2
			if lng >= mid {
				ch |= 1 << (4 - bit)
				lngRange[0] = mid
			} else {
				lngRange[1] = mid
			}
		} else {
			mid := (latRange[0] + latRange[1]) / 2
			if lat >= mid {
				ch |= 1 << (4 - bit)
				latRange[0] = mid
			} else {
				latRange[1] = mid
			}
		}
		even = !even
		if bit < 4 {
			bit++
		} else {
			hash = append(hash, base32[ch])
			bit, ch = 0, 0
		}
	}
	return string(hash)
}

// Point is a candidate location (a metro centroid).
type Point struct {
	ID    string
	Label string
	Lat   float64
	Lng   float64
}

// Nearest returns the closest point to a coordinate, or false when there are no
// candidates. Distances are compared as squared degrees, which is sufficient
// for picking a metro.
func Nearest(lat, lng float64, points []Point) (Point, bool) {
	var best Point
	bestDistance := math.Inf(1)
	found := false
	for _, p := range points {
		dLat := p.Lat - lat
		dLng := p.Lng - lng
		distance := dLat*dLat + dLng*dLng
		if distance < bestDistance {
			best, bestDistance, found = p, distance, true
		}
	}
	return best, found
}
