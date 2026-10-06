-- Replay protection for writes that clients may retry (queue, invites, connect,
-- messages). A repeated Idempotency-Key with the same body returns the stored
-- response instead of acting twice.
CREATE TABLE idempotency_keys (
    user_id      TEXT NOT NULL,
    key          TEXT NOT NULL,
    endpoint     TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status       INTEGER NOT NULL,
    response     TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (user_id, key)
);
