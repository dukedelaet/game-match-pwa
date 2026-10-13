package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

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

// BlocksBy lists the blocks a user has placed (directed, newest first).
func (s *Store) BlocksBy(ctx context.Context, blockerID string) ([]Block, error) {
	out := []Block{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM blocks WHERE blocker_id = ? ORDER BY created_at DESC`, blockerID)
	return out, err
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
	// One transaction: a partial erasure would leave the account looking deleted
	// while retaining the data the user asked us to remove.
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := NowTS()
	// The cleartext phone row and its stable hash. The hash is UNIQUE, so
	// keeping it would stop the number ever being used to register again.
	for _, q := range []string{
		`DELETE FROM user_private WHERE user_id = ?`,
		`DELETE FROM photos WHERE user_id = ?`,
		`DELETE FROM sessions WHERE user_id = ?`,
		`DELETE FROM push_subscriptions WHERE user_id = ?`,
		`DELETE FROM queue_entries WHERE user_id = ?`,
		`DELETE FROM invites WHERE from_user_id = ? OR to_user_id = ?`,
		`DELETE FROM blocks WHERE blocker_id = ? OR blocked_id = ?`,
		`DELETE FROM messages WHERE sender_id = ?`,
		`DELETE FROM round_answers WHERE user_id = ?`,
		`DELETE FROM xp_events WHERE user_id = ?`,
		`DELETE FROM signal_events WHERE user_id = ?`,
		`DELETE FROM user_intents WHERE user_id = ?`,
		`DELETE FROM user_traits WHERE user_id = ?`,
		`DELETE FROM preferences WHERE user_id = ?`,
		`DELETE FROM profiles WHERE user_id = ?`,
		`DELETE FROM user_behavior_stats WHERE user_id = ?`,
		`DELETE FROM idempotency_keys WHERE user_id = ?`,
		`DELETE FROM session_participants WHERE user_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, userID, userID); err != nil {
			return fmt.Errorf("erasing %q: %w", q, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET
			status='deleted', name='Deleted', email=NULL, phone_e164_hash=NULL,
			dob=NULL, approx_geohash=NULL, approx_geohash_source=NULL,
			incognito=0, hidden=0, last_seen_at=NULL, updated_at=?
		WHERE id=?`, now, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// bucketDigest converts a rate-limit bucket suffix (a phone number or an IP) into
// a stable, non-reversible key. The limiter's bucket column is the table's
// PRIMARY KEY, so storing the raw value made rate_limits a permanent plaintext
// index of every phone number that ever requested a code.
func BucketDigest(suffix string) string {
	sum := sha256.Sum256([]byte(suffix))
	return hex.EncodeToString(sum[:8])
}

// SweepRateLimits deletes rate-limit rows whose window started longer than
// hours ago. Without it the table only ever grew, since nothing else removes
// rows.
func (s *Store) SweepRateLimits(ctx context.Context, hours int) error {
	cutoff := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).UnixMilli()
	_, err := s.DB.ExecContext(ctx, `DELETE FROM rate_limits WHERE window_start < ?`, cutoff)
	return err
}
