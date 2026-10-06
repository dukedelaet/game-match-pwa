package store

import (
	"context"

	"github.com/google/uuid"
)

// InsertBlock records a block (idempotent per direction).
func (s *Store) InsertBlock(ctx context.Context, blockerID, blockedID string) error {
	now := NowTS()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO blocks (id, blocker_id, blocked_id, created_at, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(blocker_id, blocked_id) DO NOTHING`,
		uuid.NewString(), blockerID, blockedID, now, now)
	return err
}

// DeleteBlock removes one direction of a block.
func (s *Store) DeleteBlock(ctx context.Context, blockerID, blockedID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM blocks WHERE blocker_id = ? AND blocked_id = ?`, blockerID, blockedID)
	return err
}

// BlockedEitherWay reports whether either user has blocked the other.
func (s *Store) BlockedEitherWay(ctx context.Context, a, b string) (bool, error) {
	var n int
	err := s.DB.GetContext(ctx, &n, `
		SELECT count(*) FROM blocks
		WHERE (blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)`,
		a, b, b, a)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// InsertReport records a report.
func (s *Store) InsertReport(ctx context.Context, r *Report) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	now := NowTS()
	r.CreatedAt = &now
	r.UpdatedAt = &now
	if r.Status == "" {
		r.Status = "open"
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO reports (id, reporter_id, subject_id, reason, details, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		r.ID, r.ReporterID, r.SubjectID, r.Reason, r.Details, r.Status, r.CreatedAt, r.UpdatedAt)
	return err
}

// ReportsNewest lists recent reports.
func (s *Store) ReportsNewest(ctx context.Context, limit int) ([]Report, error) {
	out := []Report{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM reports ORDER BY created_at DESC LIMIT ?`, limit)
	return out, err
}

// SetUserStatus writes a user's status (used for moderation freezes).
func (s *Store) SetUserStatus(ctx context.Context, userID, status string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET status=?, updated_at=? WHERE id=?`, status, NowTS(), userID)
	return err
}

// HasActiveLegalHold reports whether a user is under an active legal hold.
func (s *Store) HasActiveLegalHold(ctx context.Context, userID string) (bool, error) {
	var n int
	err := s.DB.GetContext(ctx, &n, `SELECT count(*) FROM legal_holds WHERE user_id = ? AND status = 'active'`, userID)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// InsertLegalHold creates a legal hold.
func (s *Store) InsertLegalHold(ctx context.Context, h *LegalHold) error {
	if h.ID == "" {
		h.ID = uuid.NewString()
	}
	now := NowTS()
	h.CreatedAt = &now
	h.UpdatedAt = &now
	if h.Status == "" {
		h.Status = "active"
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO legal_holds (id, user_id, report_id, reason, status, photo_keys, message_ids, purge_after, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		h.ID, h.UserID, h.ReportID, h.Reason, h.Status, h.PhotoKeys, h.MessageIDs, h.PurgeAfter, h.CreatedAt, h.UpdatedAt)
	return err
}

// AnonymizeUser applies the account-deletion mutation.
func (s *Store) AnonymizeUser(ctx context.Context, userID string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE users SET status='deleted', name='Deleted', email=NULL, incognito=0, hidden=0, updated_at=? WHERE id=?`,
		NowTS(), userID)
	return err
}
