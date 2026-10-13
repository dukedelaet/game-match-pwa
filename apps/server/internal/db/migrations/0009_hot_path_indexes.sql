-- Hot-path indexes. Each of these was a full table scan under
-- EXPLAIN QUERY PLAN: matches, invites, pair_relationships, game_sessions and
-- round_answers had no index matching their actual WHERE/ORDER BY, and the
-- query plans are asserted in internal/db/indexes_test.go.
--
-- matches is searched from both sides (user_a or user_b), so it needs one
-- index per side: the UNIQUE(user_a, user_b) cannot serve a user_b lookup.
CREATE INDEX IF NOT EXISTS matches_a_idx ON matches (user_a, state, matched_at DESC);
CREATE INDEX IF NOT EXISTS matches_b_idx ON matches (user_b, state, matched_at DESC);
CREATE INDEX IF NOT EXISTS matches_exp_idx ON matches (state, expires_at) WHERE state='mutual';

CREATE INDEX IF NOT EXISTS invites_to_idx ON invites (to_user_id, state, expires_at, created_at);
CREATE INDEX IF NOT EXISTS invites_exp_idx ON invites (state, expires_at);

-- The ticker asks for sessions that are NOT completed/forfeit/cancelled, which
-- an index on state cannot serve because NOT IN never narrows a range. A
-- partial index over the live states matches the query directly.
CREATE INDEX IF NOT EXISTS gs_active_idx ON game_sessions (state)
	WHERE state NOT IN ('completed','forfeit','cancelled');

-- The chat list orders by (created_at, rowid). rowid cannot appear in an
-- index definition, but id is the text primary key and indexes it too, so the
-- lookup is ordered rather than sorted on every read.
CREATE INDEX IF NOT EXISTS messages_ts_idx ON messages (thread_id, created_at, id);

-- The scorer reads a user's whole answer history twice per completed session,
-- while holding the engine mutex.
CREATE INDEX IF NOT EXISTS ra_user_idx ON round_answers (user_id, round_id);

CREATE INDEX IF NOT EXISTS prompt_kind_idx ON prompt_bank (game_kind, active);

CREATE INDEX IF NOT EXISTS pair_u_idx ON pair_relationships (user_a, state);
CREATE INDEX IF NOT EXISTS pair_v_idx ON pair_relationships (user_b, state);