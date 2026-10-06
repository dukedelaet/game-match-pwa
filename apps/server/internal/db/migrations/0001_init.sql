-- GameMatch v1 schema (SQLite).
-- UUIDs are TEXT. Timestamps are RFC3339 text stored in UTC.
-- JSON columns are TEXT.

CREATE TABLE users (
    id                TEXT PRIMARY KEY,
    name              TEXT,
    email             TEXT UNIQUE,
    phone_e164_hash   TEXT UNIQUE,
    role              TEXT NOT NULL DEFAULT 'user',
    onboarding_step   TEXT NOT NULL DEFAULT 'welcome',
    status            TEXT NOT NULL DEFAULT 'pending',
    age_attested_at   TEXT,
    dob               TEXT,
    incognito         INTEGER NOT NULL DEFAULT 0,
    hidden            INTEGER NOT NULL DEFAULT 0,
    last_seen_at      TEXT,
    last_active_on    TEXT,
    metro_id          TEXT,
    approx_geohash    TEXT,
    xp                INTEGER NOT NULL DEFAULT 0,
    level             INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT,
    updated_at        TEXT
);
CREATE INDEX users_metro_id_idx ON users (metro_id);

CREATE TABLE user_private (
    user_id      TEXT PRIMARY KEY,
    phone_e164   TEXT,
    created_at   TEXT,
    updated_at   TEXT
);

CREATE TABLE metros (
    id            TEXT PRIMARY KEY,
    slug          TEXT NOT NULL UNIQUE,
    label         TEXT NOT NULL,
    centroid_lat  REAL,
    centroid_lng  REAL,
    adjacent_ids  TEXT,
    created_at    TEXT,
    updated_at    TEXT
);

CREATE TABLE genders (
    id     TEXT PRIMARY KEY,
    slug   TEXT NOT NULL UNIQUE,
    label  TEXT NOT NULL,
    sort   INTEGER NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE traits (
    id    TEXT PRIMARY KEY,
    slug  TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL,
    emoji TEXT,
    sort  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE profiles (
    user_id        TEXT PRIMARY KEY,
    age            INTEGER,
    city_label     TEXT,
    bio            TEXT,
    gender_id      TEXT,
    favorite_games TEXT,
    created_at     TEXT,
    updated_at     TEXT
);

CREATE TABLE user_intents (
    user_id TEXT NOT NULL,
    intent  TEXT NOT NULL,
    PRIMARY KEY (user_id, intent)
);

CREATE TABLE user_traits (
    user_id  TEXT NOT NULL,
    trait_id TEXT NOT NULL,
    PRIMARY KEY (user_id, trait_id)
);

CREATE TABLE preferences (
    user_id          TEXT PRIMARY KEY,
    age_min          INTEGER NOT NULL DEFAULT 18,
    age_max          INTEGER NOT NULL DEFAULT 99,
    distance_scope   TEXT NOT NULL DEFAULT 'metro',
    who_to_meet_open INTEGER NOT NULL DEFAULT 1,
    who_to_meet      TEXT,
    created_at       TEXT,
    updated_at       TEXT
);

CREATE TABLE photos (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL,
    path             TEXT NOT NULL,
    thumb_path       TEXT,
    moderation_state TEXT NOT NULL DEFAULT 'ok',
    blurhash         TEXT,
    created_at       TEXT,
    updated_at       TEXT
);
CREATE INDEX photos_user_id_idx ON photos (user_id);

CREATE TABLE blocks (
    id         TEXT PRIMARY KEY,
    blocker_id TEXT NOT NULL,
    blocked_id TEXT NOT NULL,
    created_at TEXT,
    updated_at TEXT,
    UNIQUE (blocker_id, blocked_id)
);
CREATE INDEX blocks_blocker_idx ON blocks (blocker_id);
CREATE INDEX blocks_blocked_idx ON blocks (blocked_id);

CREATE TABLE reports (
    id          TEXT PRIMARY KEY,
    reporter_id TEXT NOT NULL,
    subject_id  TEXT,
    reason      TEXT NOT NULL,
    details     TEXT,
    status      TEXT NOT NULL DEFAULT 'open',
    created_at  TEXT,
    updated_at  TEXT
);

CREATE TABLE legal_holds (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    report_id   TEXT,
    reason      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    photo_keys  TEXT,
    message_ids TEXT,
    purge_after TEXT,
    created_at  TEXT,
    updated_at  TEXT
);

CREATE TABLE feature_flags (
    "key" TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE allowlist_phones (
    phone_e164_hash TEXT PRIMARY KEY,
    created_at      TEXT,
    updated_at      TEXT
);

CREATE TABLE prompt_bank (
    id         TEXT PRIMARY KEY,
    game_kind  TEXT NOT NULL,
    locale     TEXT NOT NULL DEFAULT 'en',
    payload    TEXT NOT NULL,
    tags       TEXT,
    active     INTEGER NOT NULL DEFAULT 1,
    nsfw_level INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE game_sessions (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL,
    mode          TEXT NOT NULL,
    state         TEXT NOT NULL DEFAULT 'pending',
    started_at    TEXT,
    ended_at      TEXT,
    config        TEXT,
    current_round INTEGER NOT NULL DEFAULT 0,
    answer_by     TEXT,
    forfeit_after TEXT,
    created_at    TEXT,
    updated_at    TEXT
);

CREATE TABLE session_participants (
    session_id   TEXT NOT NULL,
    user_id      TEXT NOT NULL,
    seat         INTEGER NOT NULL,
    joined_at    TEXT,
    last_poll_at TEXT,
    PRIMARY KEY (session_id, user_id)
);

CREATE TABLE rounds (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    round_index INTEGER NOT NULL,
    prompt_id   TEXT,
    state       TEXT NOT NULL DEFAULT 'open',
    answer_by   TEXT,
    extra       TEXT
);
CREATE INDEX rounds_session_idx ON rounds (session_id);

CREATE TABLE round_answers (
    round_id     TEXT NOT NULL,
    user_id      TEXT NOT NULL,
    payload      TEXT NOT NULL,
    submitted_at TEXT,
    PRIMARY KEY (round_id, user_id)
);

CREATE TABLE queue_entries (
    id                 TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL UNIQUE,
    metro_id           TEXT,
    game_kind          TEXT NOT NULL,
    enqueued_at        TEXT NOT NULL,
    allow_practice     INTEGER NOT NULL DEFAULT 0,
    matched_session_id TEXT
);
CREATE INDEX queue_match_idx ON queue_entries (game_kind, metro_id, matched_session_id);

CREATE TABLE pair_relationships (
    id                  TEXT PRIMARY KEY,
    user_a              TEXT NOT NULL,
    user_b              TEXT NOT NULL,
    state               TEXT NOT NULL DEFAULT 'open_play',
    a_action            TEXT NOT NULL DEFAULT 'none',
    b_action            TEXT NOT NULL DEFAULT 'none',
    pending_expires_at  TEXT,
    cooldown_until      TEXT,
    origin_session_id   TEXT,
    created_at          TEXT,
    updated_at          TEXT,
    UNIQUE (user_a, user_b)
);

CREATE TABLE matches (
    id                TEXT PRIMARY KEY,
    user_a            TEXT NOT NULL,
    user_b            TEXT NOT NULL,
    state             TEXT NOT NULL DEFAULT 'mutual',
    origin_session_id TEXT,
    unmatched_by      TEXT,
    matched_at        TEXT,
    expires_at        TEXT,
    created_at        TEXT,
    updated_at        TEXT,
    UNIQUE (user_a, user_b)
);

CREATE TABLE chat_threads (
    id         TEXT PRIMARY KEY,
    match_id   TEXT NOT NULL UNIQUE,
    state      TEXT NOT NULL DEFAULT 'open',
    created_at TEXT,
    updated_at TEXT
);

CREATE TABLE messages (
    id         TEXT PRIMARY KEY,
    thread_id  TEXT NOT NULL,
    sender_id  TEXT,
    kind       TEXT NOT NULL DEFAULT 'text',
    body       TEXT NOT NULL,
    meta       TEXT,
    created_at TEXT,
    updated_at TEXT
);
CREATE INDEX messages_thread_idx ON messages (thread_id);

CREATE TABLE invites (
    id           TEXT PRIMARY KEY,
    from_user_id TEXT NOT NULL,
    to_user_id   TEXT NOT NULL,
    game_kind    TEXT NOT NULL,
    state        TEXT NOT NULL DEFAULT 'pending',
    expires_at   TEXT NOT NULL,
    created_at   TEXT,
    updated_at   TEXT
);

CREATE TABLE signal_events (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    session_id TEXT,
    kind       TEXT NOT NULL,
    "key"      TEXT NOT NULL,
    value      TEXT,
    created_at TEXT,
    updated_at TEXT
);
CREATE INDEX signal_events_user_idx ON signal_events (user_id);

CREATE TABLE compatibility_snapshots (
    id             TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL,
    scorer_version TEXT NOT NULL DEFAULT 'scorer_v0',
    user_a         TEXT NOT NULL,
    user_b         TEXT NOT NULL,
    score          REAL NOT NULL,
    components     TEXT,
    reasons        TEXT,
    computed_at    TEXT NOT NULL,
    UNIQUE (session_id, scorer_version)
);

CREATE TABLE xp_events (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    kind       TEXT NOT NULL,
    amount     INTEGER NOT NULL,
    created_at TEXT,
    updated_at TEXT
);
CREATE INDEX xp_events_user_idx ON xp_events (user_id);

CREATE TABLE user_behavior_stats (
    user_id    TEXT PRIMARY KEY,
    dims       TEXT,
    created_at TEXT,
    updated_at TEXT
);

-- Go-only tables (session store and OTP codes).
CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    last_seen_at TEXT
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

CREATE TABLE otp_codes (
    phone      TEXT PRIMARY KEY,
    code       TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
