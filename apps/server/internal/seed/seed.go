package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"gamematch/internal/config"
	"gamematch/internal/store"
)

type seedGender struct {
	id, slug, label string
	sort            int
}

type seedTrait struct {
	id, slug, label, emoji string
	sort                   int
}

var seedGenders = []seedGender{
	{"11111111-1111-4111-8111-111111111110", "woman", "Woman", 0},
	{"11111111-1111-4111-8111-111111111111", "man", "Man", 1},
	{"11111111-1111-4111-8111-111111111112", "non-binary", "Non-binary", 2},
	{"11111111-1111-4111-8111-111111111113", "prefer_not", "Prefer not to say", 3},
}

var seedTraits = []seedTrait{
	{"11111111-1111-4111-8111-111111111120", "competitive", "Competitive", "🔥", 0},
	{"11111111-1111-4111-8111-111111111121", "funny", "Funny", "😂", 1},
	{"11111111-1111-4111-8111-111111111122", "creative", "Creative", "🎨", 2},
	{"11111111-1111-4111-8111-111111111123", "night-owl", "Night owl", "🌙", 3},
	{"11111111-1111-4111-8111-111111111124", "music", "Music lover", "🎵", 4},
	{"11111111-1111-4111-8111-111111111125", "active", "Active", "🏋️", 5},
	{"11111111-1111-4111-8111-111111111126", "social", "Social", "🍻", 6},
	{"11111111-1111-4111-8111-111111111127", "nerdy", "Nerdy", "🧠", 7},
	{"11111111-1111-4111-8111-111111111128", "romantic", "Romantic", "❤️", 8},
	{"11111111-1111-4111-8111-111111111129", "chaotic", "Chaotic", "😈", 9},
}

const (
	laMetroID = "11111111-1111-4111-8111-111111111101"
	nyMetroID = "11111111-1111-4111-8111-111111111102"
)

// Run inserts the demo world. It is idempotent: rows use stable ids and
// INSERT OR IGNORE, so re-running is safe.
func Run(ctx context.Context, s *store.Store, cfg config.Config) error {
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := store.NowTS()
	dob := time.Now().UTC().AddDate(-28, 0, 0).Format("2006-01-02")

	if err := exec(ctx, tx, `INSERT OR IGNORE INTO metros (id, slug, label, centroid_lat, centroid_lng, adjacent_ids, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, laMetroID, "los-angeles", "Los Angeles", 34.05, -118.24, "[]", now, now); err != nil {
		return err
	}
	if err := exec(ctx, tx, `INSERT OR IGNORE INTO metros (id, slug, label, centroid_lat, centroid_lng, adjacent_ids, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, nyMetroID, "new-york", "New York", 40.71, -74.00, "[]", now, now); err != nil {
		return err
	}
	for _, g := range seedGenders {
		if err := exec(ctx, tx, `INSERT OR IGNORE INTO genders (id, slug, label, sort, active) VALUES (?,?,?,?,1)`, g.id, g.slug, g.label, g.sort); err != nil {
			return err
		}
	}
	for _, t := range seedTraits {
		if err := exec(ctx, tx, `INSERT OR IGNORE INTO traits (id, slug, label, emoji, sort) VALUES (?,?,?,?,?)`, t.id, t.slug, t.label, t.emoji, t.sort); err != nil {
			return err
		}
	}

	// this_or_that prompts
	tot := []struct {
		left, right string
		tags        []string
	}{
		{"Beach", "Mountains", []string{"interest.outdoors"}},
		{"Tacos", "Sushi", []string{"interest.food"}},
		{"Stay in", "Go out", []string{"lifestyle.social"}},
		{"Risk it", "Play it safe", []string{"behavior.risk"}},
		{"Early night", "Sunrise", []string{"lifestyle.schedule"}},
		{"Cats", "Dogs", []string{"interest.pets"}},
		{"Texts", "Calls", []string{"lifestyle.chat"}},
		{"Concert", "Museum", []string{"interest.culture"}},
		{"Coffee", "Cocktails", []string{"interest.drinks"}},
		{"Board games", "Video games", []string{"interest.games"}},
	}
	for i, row := range tot {
		payload, _ := json.Marshal(map[string]any{
			"left":  map[string]any{"id": "left", "label": row.left, "tags": row.tags},
			"right": map[string]any{"id": "right", "label": row.right, "tags": row.tags},
		})
		tags, _ := json.Marshal(row.tags)
		id := promptID(1000 + i)
		if err := exec(ctx, tx, `INSERT OR IGNORE INTO prompt_bank (id, game_kind, locale, payload, tags, active, nsfw_level)
			VALUES (?,?,?,?,?,1,0)`, id, "this_or_that", "en", string(payload), string(tags)); err != nil {
			return err
		}
	}

	// shared question sets for twenty_questions and guess_my_answer
	questions := []struct {
		question string
		options  []string
	}{
		{"Ideal first date?", []string{"Dinner", "Walk", "Arcade", "Something spontaneous"}},
		{"$10,000 first buy?", []string{"Travel", "Motorcycle", "Save it", "Party"}},
		{"Weekend vibe?", []string{"Hike", "Brunch", "Couch", "Club"}},
		{"Love language?", []string{"Time", "Words", "Gifts", "Touch"}},
		{"Karaoke song?", []string{"Power ballad", "Rap", "Sit it out", "Duet"}},
		{"Breakfast?", []string{"Savory", "Sweet", "Coffee only", "Skip it"}},
	}
	for i, row := range questions {
		options := make([]map[string]any, 0, len(row.options))
		for j, label := range row.options {
			options = append(options, map[string]any{"id": string(rune('a' + j)), "label": label, "tags": []string{}})
		}
		payload, _ := json.Marshal(map[string]any{"question": row.question, "options": options})
		if err := exec(ctx, tx, `INSERT OR IGNORE INTO prompt_bank (id, game_kind, locale, payload, tags, active, nsfw_level)
			VALUES (?,?,?,?,?,1,0)`, promptID(2000+i), "twenty_questions", "en", string(payload), "[]"); err != nil {
			return err
		}
		if err := exec(ctx, tx, `INSERT OR IGNORE INTO prompt_bank (id, game_kind, locale, payload, tags, active, nsfw_level)
			VALUES (?,?,?,?,?,1,0)`, promptID(3000+i), "guess_my_answer", "en", string(payload), "[]"); err != nil {
			return err
		}
	}

	flags := []struct{ key, value string }{
		{"auth.public_signup", `{"v":true}`},
		{"games.enabled", `{"v":true}`},
		{"staff.force_pair", `{"v":true}`},
	}
	for _, f := range flags {
		if err := exec(ctx, tx, `INSERT OR IGNORE INTO feature_flags ("key", value) VALUES (?,?)`, f.key, f.value); err != nil {
			return err
		}
	}

	if err := exec(ctx, tx, `INSERT OR IGNORE INTO users (id, name, status, onboarding_step, role, metro_id, xp, level, created_at, updated_at)
		VALUES (?,?,?,?,?,?,0,1,?,?)`, cfg.HouseUserID, "House", "active", "done", "user", laMetroID, now, now); err != nil {
		return err
	}

	alexID := "11111111-1111-4111-8111-111111111130"
	if err := insertDemoUser(ctx, tx, alexID, "Alex", "+15551111111", laMetroID, seedGenders[0].id,
		[]string{"dating", "gaming"}, firstTraits(6), dob, now); err != nil {
		return err
	}
	jordanID := "11111111-1111-4111-8111-111111111131"
	if err := insertDemoUser(ctx, tx, jordanID, "Jordan", "+15552222222", laMetroID, seedGenders[1].id,
		[]string{"dating", "socializing"}, firstTraits(6), dob, now); err != nil {
		return err
	}

	if err := exec(ctx, tx, `INSERT OR IGNORE INTO users (id, name, phone_e164_hash, status, onboarding_step, role, metro_id, age_attested_at, xp, level, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,0,1,?,?)`,
		"11111111-1111-4111-8111-111111111132", "Staff", store.HashPhone("+15550000000"),
		"active", "done", "admin", laMetroID, now, now, now); err != nil {
		return err
	}

	for _, phone := range []string{"+15551111111", "+15552222222", "+15550000000"} {
		if err := exec(ctx, tx, `INSERT OR IGNORE INTO allowlist_phones (phone_e164_hash, created_at, updated_at) VALUES (?,?,?)`,
			store.HashPhone(phone), now, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func insertDemoUser(ctx context.Context, tx *sqlx.Tx, id, name, phone, metroID, genderID string, intents, traits []string, dob, now string) error {
	if err := exec(ctx, tx, `INSERT OR IGNORE INTO users
		(id, name, phone_e164_hash, status, onboarding_step, age_attested_at, dob, metro_id, xp, level, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,40,1,?,?)`,
		id, name, store.HashPhone(phone), "active", "done", now, dob, metroID, now, now); err != nil {
		return err
	}
	if err := exec(ctx, tx, `INSERT OR IGNORE INTO user_private (user_id, phone_e164, created_at, updated_at) VALUES (?,?,?,?)`,
		id, phone, now, now); err != nil {
		return err
	}
	if err := exec(ctx, tx, `INSERT OR IGNORE INTO profiles (user_id, age, city_label, bio, gender_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`, id, 28, "Los Angeles", "I'm usually up for a round of something.", genderID, now, now); err != nil {
		return err
	}
	if err := exec(ctx, tx, `INSERT OR IGNORE INTO preferences (user_id, age_min, age_max, distance_scope, who_to_meet_open, created_at, updated_at)
		VALUES (?,21,40,'metro',1,?,?)`, id, now, now); err != nil {
		return err
	}
	for _, intent := range intents {
		if err := exec(ctx, tx, `INSERT OR IGNORE INTO user_intents (user_id, intent) VALUES (?,?)`, id, intent); err != nil {
			return err
		}
	}
	for _, traitID := range traits {
		if err := exec(ctx, tx, `INSERT OR IGNORE INTO user_traits (user_id, trait_id) VALUES (?,?)`, id, traitID); err != nil {
			return err
		}
	}
	return nil
}

func firstTraits(n int) []string {
	out := []string{}
	for i := 0; i < n && i < len(seedTraits); i++ {
		out = append(out, seedTraits[i].id)
	}
	return out
}

func promptID(seq int) string {
	return fmt.Sprintf("11111111-1111-4111-8111-11111111%04d", seq)
}

func exec(ctx context.Context, tx *sqlx.Tx, query string, args ...any) error {
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}
