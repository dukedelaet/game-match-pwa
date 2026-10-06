package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"gamematch/internal/store"
)

type patchMeBody struct {
	Name           *string   `json:"name"`
	Dob            *string   `json:"dob"`
	MetroID        *string   `json:"metro_id"`
	Bio            *string   `json:"bio"`
	GenderID       *string   `json:"gender_id"`
	AgeAttested    *bool     `json:"age_attested"`
	OnboardingStep *string   `json:"onboarding_step"`
	Intents        *[]string `json:"intents"`
	TraitIDs       *[]string `json:"trait_ids"`
	AgeMin         *int      `json:"age_min"`
	AgeMax         *int      `json:"age_max"`
	DistanceScope  *string   `json:"distance_scope"`
	WhoToMeetOpen  *bool     `json:"who_to_meet_open"`
	WhoToMeet      *[]string `json:"who_to_meet"`
	Incognito      *bool     `json:"incognito"`
	Hidden         *bool     `json:"hidden"`
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	me, err := s.publicMe(r.Context(), u)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"user": me})
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	var body patchMeBody
	if err := decodeJSON(r, &body); err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "validation", "Could not read that")
		return
	}
	if body.Name != nil && len(*body.Name) > 40 {
		WriteError(w, http.StatusUnprocessableEntity, "validation", "That name is too long")
		return
	}
	if body.Bio != nil && len(*body.Bio) > 280 {
		WriteError(w, http.StatusUnprocessableEntity, "validation", "That bio is too long")
		return
	}
	if body.AgeMin != nil && (*body.AgeMin < 18 || *body.AgeMin > 99) {
		WriteError(w, http.StatusUnprocessableEntity, "validation", "Age range must be 18-99")
		return
	}
	if body.AgeMax != nil && (*body.AgeMax < 18 || *body.AgeMax > 99) {
		WriteError(w, http.StatusUnprocessableEntity, "validation", "Age range must be 18-99")
		return
	}
	if body.DistanceScope != nil && *body.DistanceScope != "metro" && *body.DistanceScope != "metro_and_adjacent" {
		WriteError(w, http.StatusUnprocessableEntity, "validation", "Unknown distance scope")
		return
	}

	var profileAge *int
	if body.Dob != nil && *body.Dob != "" {
		age := store.AgeOfDays(*body.Dob, time.Now().UTC())
		if age < 18 {
			WriteError(w, http.StatusForbidden, "age", "You must be 18+")
			return
		}
		profileAge = &age
		u.Dob = body.Dob
	}
	if body.Name != nil && *body.Name != "" {
		u.Name = body.Name
	}
	if body.MetroID != nil && *body.MetroID != "" {
		u.MetroID = body.MetroID
	}
	if body.AgeAttested != nil && *body.AgeAttested {
		now := store.NowTS()
		u.AgeAttestedAt = &now
	}
	if body.OnboardingStep != nil && *body.OnboardingStep != "" {
		u.OnboardingStep = *body.OnboardingStep
		if *body.OnboardingStep == "done" {
			u.Status = "active"
		}
	}
	if body.Incognito != nil {
		u.Incognito = *body.Incognito
	}
	if body.Hidden != nil {
		u.Hidden = *body.Hidden
	}
	if err := s.Store.UpdateUser(ctx, u); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}

	profile, err := s.Store.ProfileFor(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if profile == nil {
		profile = &store.Profile{UserID: u.ID}
	}
	if body.Bio != nil {
		profile.Bio = body.Bio
	}
	if body.GenderID != nil {
		profile.GenderID = body.GenderID
	}
	if profileAge != nil {
		profile.Age = profileAge
	}
	if u.MetroID != nil {
		if metro, _ := s.Store.MetroByID(ctx, *u.MetroID); metro != nil {
			label := metro.Label
			profile.CityLabel = &label
		}
	}
	if err := s.Store.UpsertProfile(ctx, *profile); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}

	if body.Intents != nil {
		if err := s.Store.ReplaceIntents(ctx, u.ID, *body.Intents); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
	}
	if body.TraitIDs != nil {
		if err := s.Store.ReplaceTraits(ctx, u.ID, *body.TraitIDs); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
	}

	prefs, err := s.Store.PreferenceFor(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if prefs == nil {
		prefs = &store.Preference{UserID: u.ID, AgeMin: 18, AgeMax: 99, DistanceScope: "metro", WhoToMeetOpen: true}
	}
	if body.AgeMin != nil {
		prefs.AgeMin = *body.AgeMin
	}
	if body.AgeMax != nil {
		prefs.AgeMax = *body.AgeMax
	}
	if body.DistanceScope != nil {
		prefs.DistanceScope = *body.DistanceScope
	}
	if body.WhoToMeetOpen != nil {
		prefs.WhoToMeetOpen = *body.WhoToMeetOpen
	}
	if body.WhoToMeet != nil {
		encoded := encodeStringList(*body.WhoToMeet)
		prefs.WhoToMeet = &encoded
	}
	if err := s.Store.UpsertPreference(ctx, *prefs); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}

	updated, err := s.Store.GetUser(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	me, err := s.publicMe(ctx, updated)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"user": me})
}

func (s *Server) deleteMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	hold, err := s.Store.HasActiveLegalHold(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if hold {
		WriteError(w, http.StatusConflict, "legal_hold", "Your account cannot be deleted yet")
		return
	}
	photos, err := s.Store.PhotosFor(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	for _, p := range photos {
		_ = os.Remove(filepath.Join(s.PhotosDir, p.Path))
		if p.ThumbPath != nil {
			_ = os.Remove(filepath.Join(s.PhotosDir, *p.ThumbPath))
		}
	}
	if err := s.Store.AnonymizeUser(ctx, u.ID); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if token := sessionToken(r); token != "" {
		_ = s.Store.DeleteSession(ctx, token)
	}
	s.clearSessionCookie(w)
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type badge struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func (s *Server) meXp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	events, err := s.Store.RecentXpEvents(ctx, u.ID, 20)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	badges := []badge{}
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Kind]++
	}
	if counts["session_complete"] >= 1 {
		badges = append(badges, badge{ID: "first-game", Label: "First game"})
	}
	if counts["mutual"] >= 1 {
		badges = append(badges, badge{ID: "spark", Label: "Spark"})
	}
	if counts["first_message"] >= 1 {
		badges = append(badges, badge{ID: "icebreaker", Label: "Icebreaker"})
	}
	if u.XP >= 100 {
		badges = append(badges, badge{ID: "regular", Label: "Regular"})
	}
	dtoEvents := make([]XpEventDTO, 0, len(events))
	for _, e := range events {
		dtoEvents = append(dtoEvents, XpEventDTO{ID: e.ID, UserID: e.UserID, Kind: e.Kind, Amount: e.Amount, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"xp": u.XP, "level": u.Level, "badges": badges, "events": dtoEvents})
}

func idParam(r *http.Request) string { return chi.URLParam(r, "id") }
