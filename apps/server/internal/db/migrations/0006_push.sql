-- Web Push subscriptions. One row per browser endpoint; a user may have many.
CREATE TABLE push_subscriptions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    endpoint   TEXT NOT NULL UNIQUE,
    p256dh     TEXT NOT NULL,
    auth       TEXT NOT NULL,
    user_agent TEXT,
    created_at TEXT,
    updated_at TEXT
);
CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);
