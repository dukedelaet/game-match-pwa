package store

import (
	"context"

	"github.com/google/uuid"
)

// PushSubscription is a browser push endpoint.
type PushSubscription struct {
	ID        string  `db:"id"`
	UserID    string  `db:"user_id"`
	Endpoint  string  `db:"endpoint"`
	P256dh    string  `db:"p256dh"`
	Auth      string  `db:"auth"`
	UserAgent *string `db:"user_agent"`
	CreatedAt *string `db:"created_at"`
	UpdatedAt *string `db:"updated_at"`
}

// SavePushSubscription records (or refreshes) a browser subscription.
func (s *Store) SavePushSubscription(ctx context.Context, sub PushSubscription) error {
	if sub.ID == "" {
		sub.ID = uuid.NewString()
	}
	now := NowTS()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(endpoint) DO UPDATE SET
			user_id=excluded.user_id, p256dh=excluded.p256dh, auth=excluded.auth,
			user_agent=excluded.user_agent, updated_at=excluded.updated_at`,
		sub.ID, sub.UserID, sub.Endpoint, sub.P256dh, sub.Auth, sub.UserAgent, now, now)
	return err
}

// DeletePushSubscription removes one of the caller's endpoints.
func (s *Store) DeletePushSubscription(ctx context.Context, userID, endpoint string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ?`, userID, endpoint)
	return err
}

// PushSubscriptionsFor lists a user's subscriptions.
func (s *Store) PushSubscriptionsFor(ctx context.Context, userID string) ([]PushSubscription, error) {
	out := []PushSubscription{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM push_subscriptions WHERE user_id = ?`, userID)
	return out, err
}
