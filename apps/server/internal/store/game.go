package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// --- queue ---

// QueueDeleteForUser clears any existing queue row.
func (s *Store) QueueDeleteForUser(ctx context.Context, userID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM queue_entries WHERE user_id = ?`, userID)
	return err
}

// QueueInsert adds a queue row.
func (s *Store) QueueInsert(ctx context.Context, e *QueueEntry) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.EnqueuedAt == "" {
		e.EnqueuedAt = NowTS()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO queue_entries (id, user_id, metro_id, game_kind, enqueued_at, allow_practice, matched_session_id)
		VALUES (?,?,?,?,?,?,?)`,
		e.ID, e.UserID, e.MetroID, e.GameKind, e.EnqueuedAt, e.AllowPractice, e.MatchedSessionID)
	return err
}

// QueueEntryForUser loads a user's queue row.
func (s *Store) QueueEntryForUser(ctx context.Context, userID string) (*QueueEntry, error) {
	var e QueueEntry
	err := s.DB.GetContext(ctx, &e, `SELECT * FROM queue_entries WHERE user_id = ?`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// QueueCandidates returns unmatched waiters for a kind in the given metros.
func (s *Store) QueueCandidates(ctx context.Context, gameKind, excludeUserID string, metroIDs []string, limit int) ([]QueueEntry, error) {
	if len(metroIDs) == 0 {
		return nil, nil
	}
	q, args, err := sqlxIn(`SELECT * FROM queue_entries WHERE game_kind = ? AND user_id != ? AND matched_session_id IS NULL AND metro_id IN (?) ORDER BY enqueued_at LIMIT ?`,
		gameKind, excludeUserID, metroIDs, limit)
	if err != nil {
		return nil, err
	}
	var out []QueueEntry
	if err := s.DB.SelectContext(ctx, &out, q, args...); err != nil {
		return nil, err
	}
	return out, nil
}

// QueueMarkMatched records the session a queue row was matched into.
func (s *Store) QueueMarkMatched(ctx context.Context, entryID, sessionID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE queue_entries SET matched_session_id = ? WHERE id = ?`, sessionID, entryID)
	return err
}

// QueueDepth counts unmatched this_or_that waiters.
func (s *Store) QueueDepth(ctx context.Context, gameKind string) (int, error) {
	var n int
	err := s.DB.GetContext(ctx, &n, `SELECT count(*) FROM queue_entries WHERE matched_session_id IS NULL AND game_kind = ?`, gameKind)
	return n, err
}

// --- sessions ---

// InsertSession creates a game session.
func (s *Store) InsertSession(ctx context.Context, gs *GameSession) error {
	if gs.ID == "" {
		gs.ID = uuid.NewString()
	}
	now := NowTS()
	gs.CreatedAt = &now
	gs.UpdatedAt = &now
	if gs.State == "" {
		gs.State = "pending"
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO game_sessions (id, kind, mode, state, started_at, ended_at, config, current_round, answer_by, forfeit_after, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		gs.ID, gs.Kind, gs.Mode, gs.State, gs.StartedAt, gs.EndedAt, gs.Config, gs.CurrentRound, gs.AnswerBy, gs.ForfeitAfter, gs.CreatedAt, gs.UpdatedAt)
	return err
}

// GetSession loads a game session.
func (s *Store) GetSession(ctx context.Context, id string) (GameSession, error) {
	var gs GameSession
	err := s.DB.GetContext(ctx, &gs, `SELECT * FROM game_sessions WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return gs, ErrNotFound
	}
	return gs, err
}

// UpdateSession writes the mutable session fields.
func (s *Store) UpdateSession(ctx context.Context, gs GameSession) error {
	now := NowTS()
	gs.UpdatedAt = &now
	_, err := s.DB.ExecContext(ctx, `
		UPDATE game_sessions SET state=?, started_at=?, ended_at=?, config=?, current_round=?, answer_by=?, updated_at=?
		WHERE id=?`,
		gs.State, gs.StartedAt, gs.EndedAt, gs.Config, gs.CurrentRound, gs.AnswerBy, gs.UpdatedAt, gs.ID)
	return err
}

// PutSessionState is a small helper for state-only updates.
func (s *Store) PutSessionState(ctx context.Context, id, state string, endedAt *string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE game_sessions SET state=?, ended_at=?, updated_at=? WHERE id=?`,
		state, endedAt, NowTS(), id)
	return err
}

// InsertParticipant adds a session participant.
func (s *Store) InsertParticipant(ctx context.Context, p *Participant) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO session_participants (session_id, user_id, seat, joined_at, last_poll_at) VALUES (?,?,?,?,?)`,
		p.SessionID, p.UserID, p.Seat, p.JoinedAt, p.LastPollAt)
	return err
}

// Participants returns participants ordered by seat.
func (s *Store) Participants(ctx context.Context, sessionID string) ([]Participant, error) {
	out := []Participant{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM session_participants WHERE session_id = ? ORDER BY seat`, sessionID)
	return out, err
}

// Participant loads one participant.
func (s *Store) Participant(ctx context.Context, sessionID, userID string) (*Participant, error) {
	var p Participant
	err := s.DB.GetContext(ctx, &p, `SELECT * FROM session_participants WHERE session_id = ? AND user_id = ?`, sessionID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// MarkJoined stamps a participant's join time and poll time.
func (s *Store) MarkJoined(ctx context.Context, sessionID, userID string) error {
	now := NowTS()
	_, err := s.DB.ExecContext(ctx, `UPDATE session_participants SET joined_at=?, last_poll_at=? WHERE session_id=? AND user_id=?`,
		now, now, sessionID, userID)
	return err
}

// TouchPoll bumps a participant's last poll time.
func (s *Store) TouchPoll(ctx context.Context, sessionID, userID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE session_participants SET last_poll_at=? WHERE session_id=? AND user_id=?`,
		NowTS(), sessionID, userID)
	return err
}

// CountJoined counts participants that have joined.
func (s *Store) CountJoined(ctx context.Context, sessionID string) (int, error) {
	var n int
	err := s.DB.GetContext(ctx, &n, `SELECT count(*) FROM session_participants WHERE session_id = ? AND joined_at IS NOT NULL`, sessionID)
	return n, err
}

// ActiveSessionIDs lists non-terminal sessions.
func (s *Store) ActiveSessionIDs(ctx context.Context) ([]string, error) {
	out := []string{}
	err := s.DB.SelectContext(ctx, &out, `SELECT id FROM game_sessions WHERE state NOT IN ('completed','forfeit','cancelled')`)
	return out, err
}

// --- rounds ---

// InsertRound creates a round.
func (s *Store) InsertRound(ctx context.Context, r *Round) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO rounds (id, session_id, round_index, prompt_id, state, answer_by, extra) VALUES (?,?,?,?,?,?,?)`,
		r.ID, r.SessionID, r.RoundIndex, r.PromptID, r.State, r.AnswerBy, r.Extra)
	return err
}

// RoundByIndex loads a session round.
func (s *Store) RoundByIndex(ctx context.Context, sessionID string, index int) (*Round, error) {
	var r Round
	err := s.DB.GetContext(ctx, &r, `SELECT * FROM rounds WHERE session_id = ? AND round_index = ?`, sessionID, index)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// RoundsFor returns all rounds for a session ordered by index.
func (s *Store) RoundsFor(ctx context.Context, sessionID string) ([]Round, error) {
	out := []Round{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM rounds WHERE session_id = ? ORDER BY round_index`, sessionID)
	return out, err
}

// PutRoundState updates a round's state.
func (s *Store) PutRoundState(ctx context.Context, roundID, state string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE rounds SET state=? WHERE id=?`, state, roundID)
	return err
}

// UpsertRoundAnswer records or replaces a player's answer.
func (s *Store) UpsertRoundAnswer(ctx context.Context, a *RoundAnswer) error {
	if a.SubmittedAt == nil {
		now := NowTS()
		a.SubmittedAt = &now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO round_answers (round_id, user_id, payload, submitted_at) VALUES (?,?,?,?)
		ON CONFLICT(round_id, user_id) DO UPDATE SET payload=excluded.payload, submitted_at=excluded.submitted_at`,
		a.RoundID, a.UserID, a.Payload, a.SubmittedAt)
	return err
}

// AnswersForRound returns all answers for a round.
func (s *Store) AnswersForRound(ctx context.Context, roundID string) ([]RoundAnswer, error) {
	out := []RoundAnswer{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM round_answers WHERE round_id = ?`, roundID)
	return out, err
}

// --- invites ---

// InsertInvite creates an invite.
func (s *Store) InsertInvite(ctx context.Context, in *Invite) error {
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	now := NowTS()
	in.CreatedAt = &now
	in.UpdatedAt = &now
	if in.State == "" {
		in.State = "pending"
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO invites (id, from_user_id, to_user_id, game_kind, state, expires_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		in.ID, in.FromUserID, in.ToUserID, in.GameKind, in.State, in.ExpiresAt, in.CreatedAt, in.UpdatedAt)
	return err
}

// GetInvite loads an invite.
func (s *Store) GetInvite(ctx context.Context, id string) (Invite, error) {
	var in Invite
	err := s.DB.GetContext(ctx, &in, `SELECT * FROM invites WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return in, ErrNotFound
	}
	return in, err
}

// PutInviteState updates an invite's state.
func (s *Store) PutInviteState(ctx context.Context, id, state string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE invites SET state=?, updated_at=? WHERE id=?`, state, NowTS(), id)
	return err
}

// PendingInvitesFor lists non-expired pending invites addressed to a user.
func (s *Store) PendingInvitesFor(ctx context.Context, userID string) ([]Invite, error) {
	out := []Invite{}
	err := s.DB.SelectContext(ctx, &out, `
		SELECT * FROM invites WHERE to_user_id = ? AND state = 'pending' AND expires_at > ? ORDER BY created_at`,
		userID, NowTS())
	return out, err
}

// CountPendingPairs counts pairs in pending state involving a user.
func (s *Store) CountPendingPairs(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.DB.GetContext(ctx, &n, `
		SELECT count(*) FROM pair_relationships WHERE state = 'pending' AND (user_a = ? OR user_b = ?)`, userID, userID)
	return n, err
}

// ExpireStaleInvites marks old pending invites expired.
func (s *Store) ExpireStaleInvites(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE invites SET state='expired' WHERE state='pending' AND expires_at <= ?`, NowTS())
	return err
}

// AgeOfDays returns whole years between a dob date and today.
func AgeOfDays(dob string, now time.Time) int {
	t, err := time.Parse("2006-01-02", dob)
	if err != nil {
		return 0
	}
	years := now.Year() - t.Year()
	if now.YearDay() < t.YearDay() {
		years--
	}
	return years
}
