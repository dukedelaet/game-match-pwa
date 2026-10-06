package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// InsertXpEvent records an XP award.
func (s *Store) InsertXpEvent(ctx context.Context, userID, kind string, amount int) error {
	now := NowTS()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO xp_events (id, user_id, kind, amount, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		uuid.NewString(), userID, kind, amount, now, now)
	return err
}

// RecentXpEvents returns a user's newest XP events.
func (s *Store) RecentXpEvents(ctx context.Context, userID string, limit int) ([]XpEvent, error) {
	out := []XpEvent{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM xp_events WHERE user_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`, userID, limit)
	return out, err
}

// InsertSignalEvent records a behaviour signal.
func (s *Store) InsertSignalEvent(ctx context.Context, userID string, sessionID *string, kind, key string, value *string) error {
	now := NowTS()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO signal_events (id, user_id, session_id, kind, "key", value, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		uuid.NewString(), userID, sessionID, kind, key, value, now, now)
	return err
}

// InsertSnapshot stores a compatibility snapshot.
func (s *Store) InsertSnapshot(ctx context.Context, snap *Snapshot) error {
	if snap.ID == "" {
		snap.ID = uuid.NewString()
	}
	if snap.ScorerVersion == "" {
		snap.ScorerVersion = "scorer_v0"
	}
	if snap.ComputedAt == "" {
		snap.ComputedAt = NowTS()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO compatibility_snapshots (id, session_id, scorer_version, user_a, user_b, score, components, reasons, computed_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		snap.ID, snap.SessionID, snap.ScorerVersion, snap.UserA, snap.UserB, snap.Score, snap.Components, snap.Reasons, snap.ComputedAt)
	return err
}

// SnapshotForSession loads a session's snapshot, or nil.
func (s *Store) SnapshotForSession(ctx context.Context, sessionID string) (*Snapshot, error) {
	var snap Snapshot
	err := s.DB.GetContext(ctx, &snap, `SELECT * FROM compatibility_snapshots WHERE session_id = ? LIMIT 1`, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snap, nil
}
