package httpapi

import (
	"net/http"

	"gamematch/internal/game"
)

func (s *Server) catalogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	metros, err := s.Store.ListMetros(ctx)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	genders, err := s.Store.ListGenders(ctx)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	traits, err := s.Store.ListTraits(ctx)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}

	type metroItem struct {
		ID    string `json:"id"`
		Slug  string `json:"slug"`
		Label string `json:"label"`
	}
	type genderItem struct {
		ID    string `json:"id"`
		Slug  string `json:"slug"`
		Label string `json:"label"`
	}
	type traitItem struct {
		ID    string  `json:"id"`
		Slug  string  `json:"slug"`
		Label string  `json:"label"`
		Emoji *string `json:"emoji"`
	}
	type intentItem struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}

	metroItems := make([]metroItem, 0, len(metros))
	for _, m := range metros {
		metroItems = append(metroItems, metroItem{ID: m.ID, Slug: m.Slug, Label: m.Label})
	}
	genderItems := make([]genderItem, 0, len(genders))
	for _, g := range genders {
		genderItems = append(genderItems, genderItem{ID: g.ID, Slug: g.Slug, Label: g.Label})
	}
	traitItems := make([]traitItem, 0, len(traits))
	for _, t := range traits {
		traitItems = append(traitItems, traitItem{ID: t.ID, Slug: t.Slug, Label: t.Label, Emoji: t.Emoji})
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"metros":  metroItems,
		"genders": genderItems,
		"traits":  traitItems,
		"intents": []intentItem{
			{ID: "dating", Label: "Dating"},
			{ID: "friendship", Label: "Friendship"},
			{ID: "gaming", Label: "Gaming"},
			{ID: "socializing", Label: "Socializing"},
		},
	})
}

func (s *Server) games(w http.ResponseWriter, r *http.Request) {
	type gameItem struct {
		Kind     string `json:"kind"`
		Label    string `json:"label"`
		Protocol string `json:"protocol"`
		Rounds   int    `json:"rounds"`
		TimerSec int    `json:"timerSec"`
		Ready    bool   `json:"ready"`
	}
	items := []gameItem{}
	for _, kind := range game.RegistryOrder {
		entry := game.For(kind)
		if entry.Kind == "" {
			continue
		}
		items = append(items, gameItem{
			Kind:     entry.Kind,
			Label:    entry.Label,
			Protocol: string(entry.Protocol),
			Rounds:   entry.Rounds,
			TimerSec: int(entry.Timer.Seconds()),
			Ready:    entry.Ready,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) legal(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{
		"terms":   "GameMatch is a play-first dating prototype. Matches are introductions, not safety guarantees. Be 18+. Be kind.",
		"privacy": "We store your profile, game answers, and messages to run matchmaking. Approximate city only — never exact GPS. You can delete your account in Me.",
	})
}
