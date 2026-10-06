-- scorer_v0 support: trait classification, behaviour counters, and the latest
-- score per pair.

-- Traits are classified so the scorer can build personality/lifestyle/interest
-- sets (P/L/I).
ALTER TABLE traits ADD COLUMN axis TEXT NOT NULL DEFAULT 'personality';
UPDATE traits SET axis = 'lifestyle' WHERE slug IN ('night-owl', 'active');
UPDATE traits SET axis = 'interest'  WHERE slug IN ('music');

-- Behaviour counters feeding behavior_sim (§Behavior vector).
ALTER TABLE user_behavior_stats ADD COLUMN risk_n        INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_behavior_stats ADD COLUMN risk_sum      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_behavior_stats ADD COLUMN tempo_n       INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_behavior_stats ADD COLUMN tempo_fast    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_behavior_stats ADD COLUMN rematch_n     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_behavior_stats ADD COLUMN rematch_yes   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_behavior_stats ADD COLUMN guess_n       INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_behavior_stats ADD COLUMN guess_correct INTEGER NOT NULL DEFAULT 0;

-- Latest completed compatibility snapshot per pair.
CREATE TABLE pair_current_scores (
    user_a      TEXT NOT NULL,
    user_b      TEXT NOT NULL,
    snapshot_id TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (user_a, user_b)
);
