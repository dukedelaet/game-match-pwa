package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// GetUser loads a user by id.
func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	var u User
	err := s.DB.GetContext(ctx, &u, `SELECT * FROM users WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// UserByName loads a user by display name.
func (s *Store) UserByName(ctx context.Context, name string) (User, error) {
	var u User
	err := s.DB.GetContext(ctx, &u, `SELECT * FROM users WHERE name = ? LIMIT 1`, name)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// UserByPhoneHash loads a user by phone hash.
func (s *Store) UserByPhoneHash(ctx context.Context, hash string) (User, error) {
	var u User
	err := s.DB.GetContext(ctx, &u, `SELECT * FROM users WHERE phone_e164_hash = ?`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// UserByEmail loads a user by email.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.DB.GetContext(ctx, &u, `SELECT * FROM users WHERE email = ?`, email)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// InsertUser creates a user row, filling id and timestamps when missing.
func (s *Store) InsertUser(ctx context.Context, u *User) error {
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	now := NowTS()
	if u.CreatedAt == nil {
		u.CreatedAt = &now
	}
	if u.UpdatedAt == nil {
		u.UpdatedAt = &now
	}
	if u.Role == "" {
		u.Role = "user"
	}
	if u.Status == "" {
		u.Status = "pending"
	}
	if u.OnboardingStep == "" {
		u.OnboardingStep = "welcome"
	}
	if u.Level == 0 {
		u.Level = 1
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO users (id, name, email, phone_e164_hash, role, onboarding_step, status,
			age_attested_at, dob, incognito, hidden, last_seen_at, last_active_on, metro_id,
			approx_geohash, xp, level, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.Name, u.Email, u.PhoneE164Hash, u.Role, u.OnboardingStep, u.Status,
		u.AgeAttestedAt, u.Dob, u.Incognito, u.Hidden, u.LastSeenAt, u.LastActiveOn, u.MetroID,
		u.ApproxGeohash, u.XP, u.Level, u.CreatedAt, u.UpdatedAt)
	return err
}

// UpdateUser writes the mutable user fields.
func (s *Store) UpdateUser(ctx context.Context, u User) error {
	now := NowTS()
	u.UpdatedAt = &now
	_, err := s.DB.ExecContext(ctx, `
		UPDATE users SET name=?, email=?, role=?, onboarding_step=?, status=?, age_attested_at=?,
			dob=?, incognito=?, hidden=?, last_seen_at=?, last_active_on=?, metro_id=?, xp=?, level=?, updated_at=?
		WHERE id=?`,
		u.Name, u.Email, u.Role, u.OnboardingStep, u.Status, u.AgeAttestedAt,
		u.Dob, u.Incognito, u.Hidden, u.LastSeenAt, u.LastActiveOn, u.MetroID, u.XP, u.Level, u.UpdatedAt,
		u.ID)
	return err
}

// TouchPresence updates last-seen and last-active-day.
func (s *Store) TouchPresence(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET last_seen_at=?, last_active_on=? WHERE id=?`,
		FmtTS(now), now.Format("2006-01-02"), id)
	return err
}

// AddXP increments XP.
func (s *Store) AddXP(ctx context.Context, userID string, amount int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET xp = xp + ? WHERE id = ?`, amount, userID)
	return err
}

// SetUserXPLevel writes an absolute XP and level.
func (s *Store) SetUserXPLevel(ctx context.Context, userID string, xp, level int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET xp=?, level=? WHERE id=?`, xp, level, userID)
	return err
}

// UpsertPrivatePhone stores the phone ciphertext row.
func (s *Store) UpsertPrivatePhone(ctx context.Context, userID, phone string) error {
	now := NowTS()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_private (user_id, phone_e164, created_at, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET phone_e164=excluded.phone_e164, updated_at=excluded.updated_at`,
		userID, phone, now, now)
	return err
}

// UpdateUserLocation stores a metro and its coarse geohash. The caller passes
// only derived values; raw coordinates never reach the database.
func (s *Store) UpdateUserLocation(ctx context.Context, userID, metroID, cityLabel, geohash, source string) error {
	now := NowTS()
	if _, err := s.DB.ExecContext(ctx, `
		UPDATE users SET metro_id=?, approx_geohash=?, approx_geohash_source=?, updated_at=? WHERE id=?`,
		metroID, geohash, source, now, userID); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE profiles SET city_label=?, updated_at=? WHERE user_id=?`,
		cityLabel, now, userID)
	return err
}

// ProfileFor loads a profile, or nil when absent.
func (s *Store) ProfileFor(ctx context.Context, userID string) (*Profile, error) {
	var p Profile
	err := s.DB.GetContext(ctx, &p, `SELECT * FROM profiles WHERE user_id = ?`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertProfile inserts or updates a profile row.
func (s *Store) UpsertProfile(ctx context.Context, p Profile) error {
	now := NowTS()
	if p.CreatedAt == nil {
		p.CreatedAt = &now
	}
	p.UpdatedAt = &now
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO profiles (user_id, age, city_label, bio, gender_id, favorite_games, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET age=excluded.age, city_label=excluded.city_label, bio=excluded.bio,
			gender_id=excluded.gender_id, favorite_games=excluded.favorite_games, updated_at=excluded.updated_at`,
		p.UserID, p.Age, p.CityLabel, p.Bio, p.GenderID, p.FavoriteGames, p.CreatedAt, p.UpdatedAt)
	return err
}

// PreferenceFor loads preferences, or nil when absent.
func (s *Store) PreferenceFor(ctx context.Context, userID string) (*Preference, error) {
	var p Preference
	err := s.DB.GetContext(ctx, &p, `SELECT * FROM preferences WHERE user_id = ?`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertPreference inserts or updates preferences.
func (s *Store) UpsertPreference(ctx context.Context, p Preference) error {
	now := NowTS()
	if p.CreatedAt == nil {
		p.CreatedAt = &now
	}
	p.UpdatedAt = &now
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO preferences (user_id, age_min, age_max, distance_scope, who_to_meet_open, who_to_meet, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET age_min=excluded.age_min, age_max=excluded.age_max,
			distance_scope=excluded.distance_scope, who_to_meet_open=excluded.who_to_meet_open,
			who_to_meet=excluded.who_to_meet, updated_at=excluded.updated_at`,
		p.UserID, p.AgeMin, p.AgeMax, p.DistanceScope, p.WhoToMeetOpen, p.WhoToMeet, p.CreatedAt, p.UpdatedAt)
	return err
}

// IntentsFor returns a user's intents.
func (s *Store) IntentsFor(ctx context.Context, userID string) ([]string, error) {
	var out []string
	err := s.DB.SelectContext(ctx, &out, `SELECT intent FROM user_intents WHERE user_id = ?`, userID)
	if out == nil {
		out = []string{}
	}
	return out, err
}

// ReplaceIntents rewrites a user's intents.
func (s *Store) ReplaceIntents(ctx context.Context, userID string, intents []string) error {
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_intents WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, in := range intents {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_intents (user_id, intent) VALUES (?,?)`, userID, in); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TraitIDsFor returns a user's trait ids.
func (s *Store) TraitIDsFor(ctx context.Context, userID string) ([]string, error) {
	out := []string{}
	err := s.DB.SelectContext(ctx, &out, `SELECT trait_id FROM user_traits WHERE user_id = ?`, userID)
	return out, err
}

// ReplaceTraits rewrites a user's traits.
func (s *Store) ReplaceTraits(ctx context.Context, userID string, traitIDs []string) error {
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_traits WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, t := range traitIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_traits (user_id, trait_id) VALUES (?,?)`, userID, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PhotosFor returns a user's photos, newest first.
func (s *Store) PhotosFor(ctx context.Context, userID string) ([]Photo, error) {
	out := []Photo{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM photos WHERE user_id = ? ORDER BY created_at`, userID)
	return out, err
}

// FirstOKPhoto returns the user's first approved photo, or nil.
func (s *Store) FirstOKPhoto(ctx context.Context, userID string) (*Photo, error) {
	var p Photo
	err := s.DB.GetContext(ctx, &p, `SELECT * FROM photos WHERE user_id = ? AND moderation_state = 'ok' ORDER BY created_at LIMIT 1`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// InsertPhoto records an uploaded photo.
func (s *Store) InsertPhoto(ctx context.Context, p *Photo) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	now := NowTS()
	p.CreatedAt = &now
	p.UpdatedAt = &now
	if p.ModerationState == "" {
		p.ModerationState = "ok"
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO photos (id, user_id, path, thumb_path, moderation_state, blurhash, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		p.ID, p.UserID, p.Path, p.ThumbPath, p.ModerationState, p.Blurhash, p.CreatedAt, p.UpdatedAt)
	return err
}

// PhotoByID loads a photo.
func (s *Store) PhotoByID(ctx context.Context, id string) (Photo, error) {
	var p Photo
	err := s.DB.GetContext(ctx, &p, `SELECT * FROM photos WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}
