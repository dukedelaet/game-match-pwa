package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// --- pairs ---

// PairFor loads the pair row for an ordered pair, or nil.
func (s *Store) PairFor(ctx context.Context, a, b string) (*Pair, error) {
	var p Pair
	err := s.DB.GetContext(ctx, &p, `SELECT * FROM pair_relationships WHERE user_a = ? AND user_b = ?`, a, b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PairByID loads a pair by id.
func (s *Store) PairByID(ctx context.Context, id string) (Pair, error) {
	var p Pair
	err := s.DB.GetContext(ctx, &p, `SELECT * FROM pair_relationships WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// PairsForUser lists every pair involving a user.
func (s *Store) PairsForUser(ctx context.Context, userID string) ([]Pair, error) {
	out := []Pair{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM pair_relationships WHERE user_a = ? OR user_b = ?`, userID, userID)
	return out, err
}

// UpsertPair inserts or updates a pair row.
func (s *Store) UpsertPair(ctx context.Context, p *Pair) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	now := NowTS()
	if p.CreatedAt == nil {
		p.CreatedAt = &now
	}
	p.UpdatedAt = &now
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO pair_relationships (id, user_a, user_b, state, a_action, b_action, pending_expires_at, cooldown_until, origin_session_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_a, user_b) DO UPDATE SET state=excluded.state, a_action=excluded.a_action, b_action=excluded.b_action,
			pending_expires_at=excluded.pending_expires_at, cooldown_until=excluded.cooldown_until,
			origin_session_id=excluded.origin_session_id, updated_at=excluded.updated_at`,
		p.ID, p.UserA, p.UserB, p.State, p.AAction, p.BAction, p.PendingExpiresAt, p.CooldownUntil, p.OriginSessionID, p.CreatedAt, p.UpdatedAt)
	return err
}

// --- matches ---

// MatchFor loads the match row for an ordered pair, or nil.
func (s *Store) MatchFor(ctx context.Context, a, b string) (*Match, error) {
	var m Match
	err := s.DB.GetContext(ctx, &m, `SELECT * FROM matches WHERE user_a = ? AND user_b = ?`, a, b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MatchByID loads a match by id.
func (s *Store) MatchByID(ctx context.Context, id string) (Match, error) {
	var m Match
	err := s.DB.GetContext(ctx, &m, `SELECT * FROM matches WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// MutualMatchesFor lists matches with state mutual involving a user.
func (s *Store) MutualMatchesFor(ctx context.Context, userID string) ([]Match, error) {
	out := []Match{}
	err := s.DB.SelectContext(ctx, &out, `
		SELECT * FROM matches WHERE state = 'mutual' AND (user_a = ? OR user_b = ?) ORDER BY matched_at DESC`, userID, userID)
	return out, err
}

// UpsertMatch inserts or updates a match row.
func (s *Store) UpsertMatch(ctx context.Context, m *Match) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	now := NowTS()
	if m.CreatedAt == nil {
		m.CreatedAt = &now
	}
	m.UpdatedAt = &now
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO matches (id, user_a, user_b, state, origin_session_id, unmatched_by, matched_at, expires_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_a, user_b) DO UPDATE SET state=excluded.state, origin_session_id=excluded.origin_session_id,
			unmatched_by=excluded.unmatched_by, matched_at=excluded.matched_at, expires_at=excluded.expires_at, updated_at=excluded.updated_at`,
		m.ID, m.UserA, m.UserB, m.State, m.OriginSessionID, m.UnmatchedBy, m.MatchedAt, m.ExpiresAt, m.CreatedAt, m.UpdatedAt)
	return err
}

// PutMatchState updates a match's state (and unmatched_by).
func (s *Store) PutMatchState(ctx context.Context, id, state string, unmatchedBy *string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE matches SET state=?, unmatched_by=?, updated_at=? WHERE id=?`,
		state, unmatchedBy, NowTS(), id)
	return err
}

// --- threads & messages ---

// ThreadForMatch loads the thread for a match, or nil.
func (s *Store) ThreadForMatch(ctx context.Context, matchID string) (*Thread, error) {
	var t Thread
	err := s.DB.GetContext(ctx, &t, `SELECT * FROM chat_threads WHERE match_id = ?`, matchID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ThreadByID loads a thread.
func (s *Store) ThreadByID(ctx context.Context, id string) (Thread, error) {
	var t Thread
	err := s.DB.GetContext(ctx, &t, `SELECT * FROM chat_threads WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// InsertThread creates a chat thread.
func (s *Store) InsertThread(ctx context.Context, t *Thread) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	now := NowTS()
	t.CreatedAt = &now
	t.UpdatedAt = &now
	if t.State == "" {
		t.State = "open"
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO chat_threads (id, match_id, state, created_at, updated_at) VALUES (?,?,?,?,?)`,
		t.ID, t.MatchID, t.State, t.CreatedAt, t.UpdatedAt)
	return err
}

// PutThreadState updates a thread's state.
func (s *Store) PutThreadState(ctx context.Context, id, state string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_threads SET state=?, updated_at=? WHERE id=?`, state, NowTS(), id)
	return err
}

// PutThreadStateByMatch updates the thread of a match.
func (s *Store) PutThreadStateByMatch(ctx context.Context, matchID, state string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_threads SET state=?, updated_at=? WHERE match_id=?`, state, NowTS(), matchID)
	return err
}

// MessagesForThread returns a thread's messages oldest first.
func (s *Store) MessagesForThread(ctx context.Context, threadID string) ([]Message, error) {
	out := []Message{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM messages WHERE thread_id = ? ORDER BY created_at, rowid`, threadID)
	return out, err
}

// LastMessage returns a thread's newest message, or nil.
func (s *Store) LastMessage(ctx context.Context, threadID string) (*Message, error) {
	var m Message
	err := s.DB.GetContext(ctx, &m, `SELECT * FROM messages WHERE thread_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, threadID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// InsertMessage appends a message.
func (s *Store) InsertMessage(ctx context.Context, m *Message) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	now := NowTS()
	m.CreatedAt = &now
	m.UpdatedAt = &now
	if m.Kind == "" {
		m.Kind = "text"
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO messages (id, thread_id, sender_id, kind, body, meta, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		m.ID, m.ThreadID, m.SenderID, m.Kind, m.Body, m.Meta, m.CreatedAt, m.UpdatedAt)
	return err
}

// CountTextMessages counts text messages in a thread.
func (s *Store) CountTextMessages(ctx context.Context, threadID string) (int, error) {
	var n int
	err := s.DB.GetContext(ctx, &n, `SELECT count(*) FROM messages WHERE thread_id = ? AND kind = 'text'`, threadID)
	return n, err
}

// ClearMatchExpiry removes a match's expiry.
func (s *Store) ClearMatchExpiry(ctx context.Context, matchID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE matches SET expires_at = NULL, updated_at = ? WHERE id = ?`, NowTS(), matchID)
	return err
}
