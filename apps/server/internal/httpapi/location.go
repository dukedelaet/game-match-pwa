package httpapi

import (
	"net/http"

	"gamematch/internal/geo"
)

// setLocation maps a one-shot device coordinate to the nearest metro. The raw
// coordinate is used only for the lookup: it is never stored, returned, or
// logged, and a user outside every metro keeps their manual pick.
func (s *Server) setLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)

	var body struct {
		Lat *float64 `json:"lat"`
		Lng *float64 `json:"lng"`
	}
	_ = decodeJSON(r, &body)
	if body.Lat == nil || body.Lng == nil {
		WriteError(w, http.StatusUnprocessableEntity, "bad", "lat and lng are required")
		return
	}

	metros, err := s.Store.ListMetros(ctx)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	points := make([]geo.Point, 0, len(metros))
	for _, metro := range metros {
		if metro.CentroidLat != nil && metro.CentroidLng != nil {
			points = append(points, geo.Point{ID: metro.ID, Label: metro.Label, Lat: *metro.CentroidLat, Lng: *metro.CentroidLng})
		}
	}

	nearest, ok := geo.Nearest(*body.Lat, *body.Lng, points)
	if !ok {
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	hash := geo.Encode(*body.Lat, *body.Lng, 5)
	if err := s.Store.UpdateUserLocation(ctx, u.ID, nearest.ID, nearest.Label, hash, "geo"); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"metroId":   nearest.ID,
		"cityLabel": nearest.Label,
	})
}
