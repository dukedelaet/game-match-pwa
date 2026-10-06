package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// CreateSession mints an opaque session token row.
func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen_at) VALUES (?,?,?,?,?)`,
		token, userID, FmtTS(now), FmtTS(now.Add(ttl)), FmtTS(now))
	if err != nil {
		return "", err
	}
	return token, nil
}

// SessionUser resolves a token to a user id, honoring expiry.
func (s *Store) SessionUser(ctx context.Context, token string) (string, bool) {
	if token == "" {
		return "", false
	}
	var ses AppSession
	err := s.DB.GetContext(ctx, &ses, `SELECT * FROM sessions WHERE id = ?`, token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		return "", false
	}
	if ParseTS(ses.ExpiresAt).Before(time.Now().UTC()) {
		_ = s.DeleteSession(ctx, token)
		return "", false
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, NowTS(), token)
	return ses.UserID, true
}

// DeleteSession removes a session token.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, token)
	return err
}

// PutOTP stores a one-time code.
func (s *Store) PutOTP(ctx context.Context, phone, code string, ttl time.Duration) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO otp_codes (phone, code, expires_at) VALUES (?,?,?)
		ON CONFLICT(phone) DO UPDATE SET code=excluded.code, expires_at=excluded.expires_at`,
		NormalizePhone(phone), code, FmtTS(time.Now().UTC().Add(ttl)))
	return err
}

// TakeOTP returns and consumes a valid code.
func (s *Store) TakeOTP(ctx context.Context, phone string) (string, bool) {
	var row OTPCode
	err := s.DB.GetContext(ctx, &row, `SELECT * FROM otp_codes WHERE phone = ?`, NormalizePhone(phone))
	if err != nil {
		return "", false
	}
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM otp_codes WHERE phone = ?`, NormalizePhone(phone))
	if ParseTS(row.ExpiresAt).Before(time.Now().UTC()) {
		return "", false
	}
	return row.Code, true
}

// AllowlistHas reports whether a phone hash is allowlisted.
func (s *Store) AllowlistHas(ctx context.Context, hash string) (bool, error) {
	var n int
	if err := s.DB.GetContext(ctx, &n, `SELECT count(*) FROM allowlist_phones WHERE phone_e164_hash = ?`, hash); err != nil {
		return false, err
	}
	return n > 0, nil
}

// AllowlistAdd inserts a phone hash.
func (s *Store) AllowlistAdd(ctx context.Context, hash string) error {
	now := NowTS()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO allowlist_phones (phone_e164_hash, created_at, updated_at) VALUES (?,?,?)
		ON CONFLICT(phone_e164_hash) DO UPDATE SET updated_at=excluded.updated_at`, hash, now, now)
	return err
}

// AllowlistCount counts allowlisted phones.
func (s *Store) AllowlistCount(ctx context.Context) (int, error) {
	var n int
	err := s.DB.GetContext(ctx, &n, `SELECT count(*) FROM allowlist_phones`)
	return n, err
}

// FlagValue reads a feature flag's JSON value, or nil.
func (s *Store) FlagValue(ctx context.Context, key string) ([]byte, error) {
	var raw string
	err := s.DB.GetContext(ctx, &raw, `SELECT value FROM feature_flags WHERE "key" = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []byte(raw), nil
}

// FlagOn reports whether a boolean feature flag is enabled.
// Values are stored as {"v": true}.
func (s *Store) FlagOn(ctx context.Context, key string) bool {
	raw, err := s.FlagValue(ctx, key)
	if err != nil || len(raw) == 0 {
		return false
	}
	var v struct {
		V bool `json:"v"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	return v.V
}

// SetFlag writes a boolean feature flag.
func (s *Store) SetFlag(ctx context.Context, key string, on bool) error {
	v := `{"v":false}`
	if on {
		v = `{"v":true}`
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO feature_flags ("key", value) VALUES (?,?)
		ON CONFLICT("key") DO UPDATE SET value=excluded.value`, key, v)
	return err
}
