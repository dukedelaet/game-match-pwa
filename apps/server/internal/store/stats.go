package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// BehaviorStatsFor loads a user's behavior counters, zeroed when absent.
func (s *Store) BehaviorStatsFor(ctx context.Context, userID string) (BehaviorStats, error) {
	var stats BehaviorStats
	err := s.DB.GetContext(ctx, &stats, `
		SELECT user_id, risk_n, risk_sum, tempo_n, tempo_fast, rematch_n, rematch_yes, guess_n, guess_correct
		FROM user_behavior_stats WHERE user_id = ?`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return BehaviorStats{UserID: userID}, nil
	}
	if err != nil {
		return BehaviorStats{}, err
	}
	return stats, nil
}

// AddBehaviorStats adds deltas to a user's counters, creating the row if needed.
func (s *Store) AddBehaviorStats(ctx context.Context, delta BehaviorStats) error {
	if delta.UserID == "" {
		return errors.New("behavior stats need a user")
	}
	now := NowTS()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_behavior_stats
			(user_id, dims, risk_n, risk_sum, tempo_n, tempo_fast, rematch_n, rematch_yes, guess_n, guess_correct, created_at, updated_at)
		VALUES (?, NULL, ?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET
			risk_n        = user_behavior_stats.risk_n + excluded.risk_n,
			risk_sum      = user_behavior_stats.risk_sum + excluded.risk_sum,
			tempo_n       = user_behavior_stats.tempo_n + excluded.tempo_n,
			tempo_fast    = user_behavior_stats.tempo_fast + excluded.tempo_fast,
			rematch_n     = user_behavior_stats.rematch_n + excluded.rematch_n,
			rematch_yes   = user_behavior_stats.rematch_yes + excluded.rematch_yes,
			guess_n       = user_behavior_stats.guess_n + excluded.guess_n,
			guess_correct = user_behavior_stats.guess_correct + excluded.guess_correct,
			updated_at    = excluded.updated_at`,
		delta.UserID,
		delta.RiskN, delta.RiskSum, delta.TempoN, delta.TempoFast,
		delta.RematchN, delta.RematchYes, delta.GuessN, delta.GuessCorrect,
		now, now)
	return err
}

// UpsertPairScore records the newest snapshot for a pair.
func (s *Store) UpsertPairScore(ctx context.Context, a, b, snapshotID string) error {
	now := NowTS()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO pair_current_scores (user_a, user_b, snapshot_id, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(user_a, user_b) DO UPDATE SET snapshot_id=excluded.snapshot_id, updated_at=excluded.updated_at`,
		a, b, snapshotID, now)
	return err
}

// TaggedAnswerCount counts a user's non-practice answers. It is the signal gate
// for showing a compatibility percent.
func (s *Store) TaggedAnswerCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.DB.GetContext(ctx, &n, `
		SELECT count(*) FROM round_answers ra
		JOIN rounds r ON r.id = ra.round_id
		JOIN game_sessions gs ON gs.id = r.session_id
		WHERE ra.user_id = ? AND gs.mode != 'practice'`, userID)
	return n, err
}

// TraitSlugsByAxis returns a user's trait slugs grouped by axis.
func (s *Store) TraitSlugsByAxis(ctx context.Context, userID string) (map[string][]string, error) {
	rows := []struct {
		Slug string `db:"slug"`
		Axis string `db:"axis"`
	}{}
	err := s.DB.SelectContext(ctx, &rows, `
		SELECT t.slug, t.axis FROM user_traits ut JOIN traits t ON t.id = ut.trait_id WHERE ut.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{"personality": {}, "lifestyle": {}, "interest": {}}
	for _, row := range rows {
		axis := row.Axis
		if axis == "" {
			axis = "personality"
		}
		out[axis] = append(out[axis], row.Slug)
	}
	return out, nil
}

// FavoriteGames returns the game slugs stored on a user's profile.
func (s *Store) FavoriteGames(ctx context.Context, userID string) ([]string, error) {
	var raw *string
	err := s.DB.GetContext(ctx, &raw, `SELECT favorite_games FROM profiles WHERE user_id = ?`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(*raw), &out); err != nil {
		return nil, nil
	}
	return out, nil
}
