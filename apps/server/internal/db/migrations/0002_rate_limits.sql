-- Fixed-window rate limit counters. Persisted so a restart cannot reset an
-- abuse window. window_start is Unix milliseconds so comparisons are numeric.
CREATE TABLE rate_limits (
    bucket       TEXT PRIMARY KEY,
    count        INTEGER NOT NULL DEFAULT 0,
    window_start INTEGER NOT NULL
);
