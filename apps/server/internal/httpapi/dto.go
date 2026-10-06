package httpapi

import (
	"context"
	"encoding/json"

	"gamematch/internal/store"
)

// PublicUser is the shape returned for the signed-in user.
type PublicUser struct {
	ID             string          `json:"id"`
	Name           *string         `json:"name"`
	OnboardingStep string          `json:"onboardingStep"`
	Status         string          `json:"status"`
	Role           string          `json:"role"`
	XP             int             `json:"xp"`
	Level          int             `json:"level"`
	Incognito      bool            `json:"incognito"`
	MetroID        *string         `json:"metroId"`
	Profile        *ProfileDTO     `json:"profile"`
	Photos         []PhotoDTO      `json:"photos"`
	Intents        []string        `json:"intents"`
	Preferences    *PreferencesDTO `json:"preferences"`
}

// ProfileDTO mirrors the profiles row.
type ProfileDTO struct {
	UserID        string          `json:"user_id"`
	Age           *int            `json:"age"`
	CityLabel     *string         `json:"city_label"`
	Bio           *string         `json:"bio"`
	GenderID      *string         `json:"gender_id"`
	FavoriteGames json.RawMessage `json:"favorite_games"`
	CreatedAt     *string         `json:"created_at"`
	UpdatedAt     *string         `json:"updated_at"`
}

// PreferencesDTO mirrors the preferences row.
type PreferencesDTO struct {
	UserID        string          `json:"user_id"`
	AgeMin        int             `json:"age_min"`
	AgeMax        int             `json:"age_max"`
	DistanceScope string          `json:"distance_scope"`
	WhoToMeetOpen bool            `json:"who_to_meet_open"`
	WhoToMeet     json.RawMessage `json:"who_to_meet"`
	CreatedAt     *string         `json:"created_at"`
	UpdatedAt     *string         `json:"updated_at"`
}

// PhotoDTO is a user's photo reference.
type PhotoDTO struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	State string `json:"state"`
}

// Card is the compact other-user summary.
type Card struct {
	ID       string  `json:"id"`
	Name     *string `json:"name"`
	Age      *int    `json:"age"`
	Bio      *string `json:"bio"`
	PhotoURL *string `json:"photoUrl"`
}

// InviteItem is an invite as seen by one user.
type InviteItem struct {
	ID        string `json:"id"`
	GameKind  string `json:"gameKind"`
	State     string `json:"state"`
	ExpiresAt string `json:"expiresAt"`
	FromMe    bool   `json:"fromMe"`
	Other     *Card  `json:"other"`
}

// PairItem is a pair as seen by one user.
type PairItem struct {
	ID             string `json:"id"`
	State          string `json:"state"`
	YourAction     string `json:"yourAction"`
	TheyConnected  bool   `json:"theyConnected"`
	InviteEligible bool   `json:"inviteEligible"`
	Other          *Card  `json:"other"`
}

// ThreadItem is a chat thread summary.
type ThreadItem struct {
	ThreadID *string      `json:"threadId"`
	MatchID  string       `json:"matchId"`
	State    *string      `json:"state"`
	Other    *Card        `json:"other"`
	Last     *LastMessage `json:"last"`
}

// LastMessage is the newest message preview.
type LastMessage struct {
	Body string  `json:"body"`
	Kind string  `json:"kind"`
	At   *string `json:"at"`
}

// MessageItem is one chat message.
type MessageItem struct {
	ID        string          `json:"id"`
	SenderID  *string         `json:"senderId"`
	Kind      string          `json:"kind"`
	Body      string          `json:"body"`
	Meta      json.RawMessage `json:"meta"`
	CreatedAt *string         `json:"createdAt"`
}

// XpEventDTO mirrors an xp_events row.
type XpEventDTO struct {
	ID        string  `json:"id"`
	UserID    string  `json:"user_id"`
	Kind      string  `json:"kind"`
	Amount    int     `json:"amount"`
	CreatedAt *string `json:"created_at"`
	UpdatedAt *string `json:"updated_at"`
}

// ReportDTO mirrors a reports row.
type ReportDTO struct {
	ID         string  `json:"id"`
	ReporterID string  `json:"reporter_id"`
	SubjectID  *string `json:"subject_id"`
	Reason     string  `json:"reason"`
	Details    *string `json:"details"`
	Status     string  `json:"status"`
	CreatedAt  *string `json:"created_at"`
	UpdatedAt  *string `json:"updated_at"`
}

func profileDTO(p *store.Profile) *ProfileDTO {
	if p == nil {
		return nil
	}
	return &ProfileDTO{
		UserID:        p.UserID,
		Age:           p.Age,
		CityLabel:     p.CityLabel,
		Bio:           p.Bio,
		GenderID:      p.GenderID,
		FavoriteGames: jsonOrNull(p.FavoriteGames),
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

func preferencesDTO(p *store.Preference) *PreferencesDTO {
	if p == nil {
		return nil
	}
	return &PreferencesDTO{
		UserID:        p.UserID,
		AgeMin:        p.AgeMin,
		AgeMax:        p.AgeMax,
		DistanceScope: p.DistanceScope,
		WhoToMeetOpen: p.WhoToMeetOpen,
		WhoToMeet:     jsonOrNull(p.WhoToMeet),
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

func jsonOrNull(s *string) json.RawMessage {
	if s == nil || *s == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(*s)
}

func encodeStringList(v []string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Server) publicMe(ctx context.Context, u store.User) (PublicUser, error) {
	profile, err := s.Store.ProfileFor(ctx, u.ID)
	if err != nil {
		return PublicUser{}, err
	}
	photos, err := s.Store.PhotosFor(ctx, u.ID)
	if err != nil {
		return PublicUser{}, err
	}
	intents, err := s.Store.IntentsFor(ctx, u.ID)
	if err != nil {
		return PublicUser{}, err
	}
	prefs, err := s.Store.PreferenceFor(ctx, u.ID)
	if err != nil {
		return PublicUser{}, err
	}
	photoDTOs := make([]PhotoDTO, 0, len(photos))
	for _, p := range photos {
		photoDTOs = append(photoDTOs, PhotoDTO{ID: p.ID, URL: "/v1/photos/" + p.ID, State: p.ModerationState})
	}
	return PublicUser{
		ID:             u.ID,
		Name:           u.Name,
		OnboardingStep: u.OnboardingStep,
		Status:         u.Status,
		Role:           u.Role,
		XP:             u.XP,
		Level:          u.Level,
		Incognito:      u.Incognito,
		MetroID:        u.MetroID,
		Profile:        profileDTO(profile),
		Photos:         photoDTOs,
		Intents:        intents,
		Preferences:    preferencesDTO(prefs),
	}, nil
}

func (s *Server) card(ctx context.Context, userID string) (*Card, error) {
	u, err := s.Store.GetUser(ctx, userID)
	if err != nil {
		return nil, nil
	}
	profile, err := s.Store.ProfileFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	photo, err := s.Store.FirstOKPhoto(ctx, userID)
	if err != nil {
		return nil, err
	}
	card := &Card{ID: u.ID, Name: u.Name}
	if profile != nil {
		card.Age = profile.Age
		card.Bio = profile.Bio
	}
	if photo != nil {
		url := "/v1/photos/" + photo.ID
		card.PhotoURL = &url
	}
	return card, nil
}

func (s *Server) inviteItem(ctx context.Context, inv store.Invite, viewerID string) (*InviteItem, error) {
	otherID := inv.FromUserID
	if inv.FromUserID == viewerID {
		otherID = inv.ToUserID
	}
	card, err := s.card(ctx, otherID)
	if err != nil {
		return nil, err
	}
	return &InviteItem{
		ID:        inv.ID,
		GameKind:  inv.GameKind,
		State:     inv.State,
		ExpiresAt: inv.ExpiresAt,
		FromMe:    inv.FromUserID == viewerID,
		Other:     card,
	}, nil
}
