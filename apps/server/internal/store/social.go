package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

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

// ExpireStaleMatches moves matches past expires_at to expired, along with their
// pair (cooldown cleared, so they may re-queue) and thread (locked).
func (s *Store) ExpireStaleMatches(ctx context.Context) error {
	now := NowTS()
	var stale []Match
	if err := s.DB.SelectContext(ctx, &stale, `
		SELECT * FROM matches WHERE state = 'mutual' AND expires_at IS NOT NULL AND expires_at <= ?`, now); err != nil {
		return err
	}
	for _, m := range stale {
		if err := s.PutMatchState(ctx, m.ID, "expired", nil); err != nil {
			return err
		}
		if _, err := s.DB.ExecContext(ctx, `
			UPDATE pair_relationships SET state='expired', cooldown_until=NULL, updated_at=?
			WHERE user_a=? AND user_b=? AND state='mutual'`, now, m.UserA, m.UserB); err != nil {
			return err
		}
		if err := s.PutThreadStateByMatch(ctx, m.ID, "locked"); err != nil {
			return err
		}
	}
	return nil
}

// CancelPendingInvites cancels pending invites between two users in either
// direction, as required when one of them blocks the other.
func (s *Store) CancelPendingInvites(ctx context.Context, a, b string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE invites SET state='cancelled', updated_at=?
		WHERE state='pending' AND ((from_user_id=? AND to_user_id=?) OR (from_user_id=? AND to_user_id=?))`,
		NowTS(), a, b, b, a)
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

// DefaultListLimit bounds every list endpoint. Without a limit these returned
// the user's entire history in one response.
const DefaultListLimit = 50

// MaxListLimit is the largest a caller may request.
const MaxListLimit = 100

// ClampListLimit normalises a caller-supplied limit: anything zero, negative or
// unparseable falls back to the default, and anything above the maximum is
// capped rather than honoured. A value too large for an int is treated as
// "very large" rather than unparseable, so ?limit=999999999999999999999 means
// "as much as you allow" instead of the default.
func ClampListLimit(raw string) int {
	if raw == "" {
		return DefaultListLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		// Overflow, or not a number. A leading digit decides which: a huge
		// number is a clamped request, anything else is junk.
		for _, r := range raw {
			if r < '0' || r > '9' {
				return DefaultListLimit
			}
		}
		if len(raw) > 1 && raw[0] == '0' {
			return DefaultListLimit
		}
		return MaxListLimit
	}
	if n <= 0 {
		return DefaultListLimit
	}
	if n > MaxListLimit {
		return MaxListLimit
	}
	return n
}

// MutualMatchesForLimited is MutualMatchesFor with a row cap.
func (s *Store) MutualMatchesForLimited(ctx context.Context, userID string, limit int) ([]Match, error) {
	out := []Match{}
	err := s.DB.SelectContext(ctx, &out, `
		SELECT * FROM matches WHERE state = 'mutual' AND (user_a = ? OR user_b = ?)
		ORDER BY matched_at DESC LIMIT ?`, userID, userID, limit)
	return out, err
}

// PairsForUserLimited is PairsForUser with a row cap.
func (s *Store) PairsForUserLimited(ctx context.Context, userID string, limit int) ([]Pair, error) {
	out := []Pair{}
	err := s.DB.SelectContext(ctx, &out, `
		SELECT * FROM pair_relationships WHERE user_a = ? OR user_b = ?
		ORDER BY created_at DESC LIMIT ?`, userID, userID, limit)
	return out, err
}

// ThreadsForMatches loads every thread for a set of matches in one query, keyed
// by match ID. Threads were previously fetched one at a time.
func (s *Store) ThreadsForMatches(ctx context.Context, matchIDs []string) (map[string]*Thread, error) {
	out := make(map[string]*Thread, len(matchIDs))
	if len(matchIDs) == 0 {
		return out, nil
	}
	var rows []Thread
	q, args, err := sqlxIn(`SELECT * FROM chat_threads WHERE match_id IN (?)`, matchIDs)
	if err != nil {
		return nil, err
	}
	if err := s.DB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	for i := range rows {
		r := rows[i]
		out[r.MatchID] = &r
	}
	return out, nil
}

// PairsForPeople loads pair relationships for a set of ordered (a,b) pairs in
// one query. Pairs were previously fetched one at a time.
//
// The pairs are matched with OR'd comparisons rather than a row-value IN list:
// sqlx.In cannot expand a variadic number of arguments inside a tuple, which
// SQLite rejects at runtime with "IN(...) element has 1 term - expected 2".
func (s *Store) PairsForPeople(ctx context.Context, pairs [][2]string) (map[string]*Pair, error) {
	out := make(map[string]*Pair, len(pairs))
	if len(pairs) == 0 {
		return out, nil
	}
	clauses := make([]string, 0, len(pairs))
	args := make([]any, 0, len(pairs)*2)
	for _, p := range pairs {
		clauses = append(clauses, "(user_a = ? AND user_b = ?)")
		args = append(args, p[0], p[1])
	}
	var rows []Pair
	query := `SELECT * FROM pair_relationships WHERE ` + strings.Join(clauses, " OR ")
	if err := s.DB.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	for i := range rows {
		r := rows[i]
		out[PairKey(r.UserA, r.UserB)] = &r
	}
	return out, nil
}

// PairKey identifies an ordered pair of users.
func PairKey(a, b string) string { return a + "\x00" + b }

// LastMessagesForThreads returns the most recent message for each thread in one
// query. LastMessage issued one statement per thread.
func (s *Store) LastMessagesForThreads(ctx context.Context, threadIDs []string) (map[string]*Message, error) {
	out := make(map[string]*Message, len(threadIDs))
	if len(threadIDs) == 0 {
		return out, nil
	}
	var rows []Message
	q, args, err := sqlxIn(`
		SELECT m.* FROM messages m
		JOIN (SELECT thread_id, MAX(created_at) AS newest FROM messages
		      WHERE thread_id IN (?) GROUP BY thread_id) latest
		  ON m.thread_id = latest.thread_id AND m.created_at = latest.newest`,
		threadIDs)
	if err != nil {
		return nil, err
	}
	if err := s.DB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	for i := range rows {
		r := rows[i]
		existing, seen := out[r.ThreadID]
		if !seen || messageNewer(&r, existing) {
			cp := r
			out[r.ThreadID] = &cp
		}
	}
	return out, nil
}

// messageNewer mirrors LastMessage's "ORDER BY created_at DESC, rowid DESC".
// rowid is monotonic within a table, so comparing ids is a stable proxy.
func messageNewer(a, b *Message) bool {
	as, bs := "", ""
	if a.CreatedAt != nil {
		as = *a.CreatedAt
	}
	if b.CreatedAt != nil {
		bs = *b.CreatedAt
	}
	if as != bs {
		return as > bs
	}
	return a.ID > b.ID
}
