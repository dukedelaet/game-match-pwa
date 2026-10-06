# Go + SQLite Backend Port Implementation Plan

> **Status: DONE — historical document (2026-10-06). Do not implement this plan.**
>
> The backend was ported directly to Go + SQLite. `apps/api` (Laravel/PHP) and
> `docker-compose.yml` (MySQL) no longer exist; the Go service lives at
> `apps/server`. Where the body below disagrees with the tree, **the tree wins**:
>
> - Design Decision 2 ("Laravel stays at `apps/api`") and Decision 4 ("golden
>   fixtures") were **not** followed — there is no `apps/api` and no
>   `testdata/golden/`.
> - Task 3 (capture Laravel responses) and Task 21 (parity against fixtures)
>   were replaced by `apps/server/internal/httpapi/api_test.go`, which covers
>   auth, onboarding, a full game, connect/mutual, chat, blocking, reports, and
>   staff tools.
> - Task 24 ("Remove Laravel") is already done.
>
> Read it as a rationale/decisions record only. For the current stack and
> commands see `README.md`, `AGENTS.md`, and the "Tech stack recommendation"
> table in `docs/design-docs-and-wireframes.md`.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Laravel/PHP + MySQL backend with a Go + SQLite service that serves the existing `/v1` REST contract unchanged, so the React PWA in `apps/web` needs no rewrite.

**Architecture:** One stateless HTTP process (chi router, `net/http`) in `apps/server` that owns a single SQLite file (WAL mode) and a photo directory on disk. Session-cookie auth (opaque token in a `sessions` row, HttpOnly cookie), OTP + demo OAuth, a polling game engine (no WebSocket), and an in-process ticker that advances/expires sessions (replacing Hostinger cron). The Laravel app stays in `apps/api` as the behavioral reference until parity is proven, then is deleted.

**Tech Stack:** Go (>=1.24), `github.com/go-chi/chi/v5`, `github.com/jmoiron/sqlx`, `modernc.org/sqlite` (pure Go, no cgo), `github.com/google/uuid`, `github.com/disintegration/imaging`, `github.com/stretchr/testify`. Frontend unchanged: Vite 8 + React 19 + TanStack Router/Query + Zustand + Tailwind 4 + `vite-plugin-pwa`.

**Spec:** `docs/design-docs-and-wireframes.md` (product/behavior intent) and the running Laravel app in `apps/api` (the authoritative current behavior). Where the two disagree, the code wins and the disagreement is listed in Design Decisions.

## Global Constraints

- **Base path:** every route is under `/v1` (Laravel `apiPrefix: 'v1'`). Keep `/v1/healthz` returning `{"ok":true}`.
- **Error body:** `{"error":{"code":"<code>","message":"<msg>"}}` with the same status codes as the Laravel controller.
- **Auth:** cookie session (`credentials: 'include'` on the client). No bearer tokens, no CSRF token (Laravel exempts `v1/*`).
- **No WebSocket, no Redis, no second process.** Clients poll (`/queue/status` 2s, `/sessions/:id` 1Hz, `/threads/:id/messages` 2s).
- **Never expose:** `dob`, `phone_e164`, `phone_e164_hash`, `approx_geohash`, `metro.centroid_lat/lng`, `metro.adjacent_ids`, `user_private`.
- **UUIDs are strings** (`TEXT` PKs); generate with `github.com/google/uuid`.
- **Timestamps:** store UTC as RFC3339 text; DTO timestamps are ISO8601 with offset (`toIso8601String()`).
- **XP constants (server-authoritative):** session complete 50, practice complete 5, mutual 20, first message 10, forfeit/cancelled 0. Level `n` requires `100*n*(n-1)/2` cumulative XP (matches `GameEngine::levelForXp`).
- **House bot:** `HOUSE_USER_ID` (default `00000000-0000-4000-8000-000000000001`), answers option index 0 / `left`, earns no XP, `pairId` is null on completion, never appears in `GET /users`.
- **Dev OTP:** when `APP_DEBUG=true` the OTP is always `123456` and `/auth/otp/start` returns `devCode`. Otherwise a random 6-digit code, cached 15 minutes.
- **Photos:** max 10 MB; mime `jpeg|jpg|png|webp`; longest edge downscaled to 1080px, re-encoded JPEG q82, plus a 320px-wide thumb q75.
- **Defaults:** `game_kind` `this_or_that`; practice fallback after 30s waited; metro expansion after 32s waited; session `pending` → `cancelled` after 30s; poll stale after 15s → `forfeit`; pair cooldown 14 days; pair pending window 72h; invite TTL 2 minutes; match `expires_at` now+7d.
- **Do not** add tables/columns the current app does not use (no `badges`, `push_subscriptions`, `auth_identities`, `games_catalog`, `pair_current_scores`). No UGC prompts.

## Design Decisions (confirm before implementing)

These are the choices the plan locks in. They are listed up front because an implementer cannot infer them from the code.

1. **Frontend is already React.** `apps/web` is React 19 + Vite. "React frontend" needs **no rewrite**; only contract drift (if any) is fixed. The Go server takes over port `8000` so `vite.config.ts`'s `/v1` proxy is untouched.
2. **Backend lands at `apps/server`, not `apps/api`.** Laravel stays at `apps/api` as a runnable reference during the port; the final task deletes it and updates docs. This is the only way to diff Go responses against a live reference.
3. **Parity means the Laravel code, not the design doc.** The doc describes unbuilt features (two-phase Guess My Answer, 20s 20-Questions timer, rate limits, idempotency keys, behavior stats). The prototype implements single-phase GMA, a 12s timer for non-`this_or_that`, and no rate limiting. **Port the code.** Doc-only features become follow-up work, one optional task each.
4. **Golden-response contract tests.** Before deleting Laravel, capture its responses for the key endpoints into `apps/server/testdata/golden/*.json` and assert the Go server produces equivalent JSON (IDs/timestamps normalized). This is the spine of correctness.
5. **SQLite access:** `modernc.org/sqlite`, `PRAGMA journal_mode=WAL`, `PRAGMA busy_timeout=5000`, `PRAGMA foreign_keys=ON`, one `*sql.DB` with `SetMaxOpenConns(1)` (SQLite serializes writers). The engine's read-modify-write transitions run inside `BEGIN IMMEDIATE` transactions.
6. **Sessions/auth:** a `sessions` table (`id`, `user_id`, `created_at`, `expires_at`, `last_seen_at`) plus an `otp_codes` table replace Laravel's session store and `Cache`-based OTP. Cookie name `gm_session`, `HttpOnly`, `SameSite=Lax`, `Secure` when `APP_ENV != local`.
7. **Background work:** a `time.Ticker` (5s) goroutine calls `Engine.Advance` for non-terminal sessions, replacing `routes/console.php`'s per-minute schedule. The per-request `Advance` calls stay (that is what makes polling timely).
8. **Open question (needs an answer before deploy):** Go cannot run on Hostinger Business shared hosting the way PHP did. Deployment target must become a VPS/binary host (or a container). The port itself is host-agnostic; only the runbook changes.

## File Structure

```
apps/server/
  go.mod, go.sum
  cmd/gamematch/main.go              # wiring: config, db, engine, router, ticker, listen
  internal/config/config.go          # env load (ports, APP_DEBUG, APP_ENV, HOUSE_USER_ID, data dir)
  internal/db/db.go                  # sqlx handle, PRAGMAs, tx helper
  internal/db/migrate.go             # embed.FS + PRAGMA user_version runner
  internal/db/migrations/0001_init.sql
  internal/store/store.go            # Store struct wrapping *sqlx.DB
  internal/store/users.go            # users, user_private, profiles, preferences, intents, traits
  internal/store/auth.go             # sessions, otp_codes, allowlist
  internal/store/photos.go           # photos rows
  internal/store/catalog.go          # metros, genders, traits, prompt_bank, feature_flags
  internal/store/queue.go            # queue_entries
  internal/store/sessions.go         # game_sessions, session_participants, rounds, round_answers
  internal/store/pairs.go            # pair_relationships, matches, chat_threads, messages, invites
  internal/store/safety.go           # blocks, reports, legal_holds
  internal/store/xp.go               # xp_events, signal_events, compatibility_snapshots
  internal/game/engine.go            # start/join/heartbeat/answer/advance/view
  internal/game/matcher.go           # enqueue/tick/leave + hard filters
  internal/game/scorer.go            # scorer_v0 + snapshot + signals
  internal/game/policy.go            # BlockedEitherWay, IntentsOK, AgeOK, GenderOK, OrderedPair, LevelForXP
  internal/media/photos.go           # decode, downscale, thumb, JPEG encode
  internal/seed/seed.go              # metros/genders/traits/prompts/flags/house/demo users/allowlist
  internal/httpapi/router.go         # route table (mirrors routes/api.php)
  internal/httpapi/respond.go        # JSON + error envelope helpers
  internal/httpapi/middleware.go     # session auth, rate limit, request logging, recover
  internal/httpapi/auth.go           # otp start/verify, oauth demo, logout, session
  internal/httpapi/me.go             # GET/PATCH /me, DELETE /me, /me/xp, publicMe DTO
  internal/httpapi/photos.go         # POST /me/photos, GET /photos/:id
  internal/httpapi/catalog.go        # /catalogs, /games, /legal
  internal/httpapi/home.go           # /home
  internal/httpapi/queue.go          # /queue, /queue/status
  internal/httpapi/sessions.go       # join/show/answer/leave/rematch
  internal/httpapi/pairs.go          # /pairs, /pairs/:id, connect, unmatch
  internal/httpapi/threads.go        # /threads, messages
  internal/httpapi/invites.go        # /invites create/accept/decline
  internal/httpapi/safety.go         # /blocks, /reports
  internal/httpapi/staff.go          # /staff/allowlist, /internal/*
  internal/httpapi/dto.go            # response DTOs (golden-tested)
  testdata/golden/*.json             # captured Laravel responses
  data/                              # runtime: gamematch.db, photos/ (git-ignored)
```

## Ported Schema (SQLite, `0001_init.sql`)

Column-for-column parity with `apps/api/database/migrations/*`. `uuid` → `TEXT NOT NULL`, `json('x')` → `TEXT`, booleans → `INTEGER NOT NULL DEFAULT 0|1`, `unsignedInteger` → `INTEGER`, `timestamp` → `TEXT`.

- `users(id PK, name, email UNIQUE, phone_e164_hash UNIQUE, role DEFAULT 'user', onboarding_step DEFAULT 'welcome', status DEFAULT 'pending', age_attested_at, dob, incognito DEFAULT 0, hidden DEFAULT 0, last_seen_at, last_active_on, metro_id, approx_geohash, xp DEFAULT 0, level DEFAULT 1, created_at, updated_at)`
- `user_private(user_id PK, phone_e164, created_at, updated_at)`
- `metros(id PK, slug UNIQUE, label, centroid_lat REAL, centroid_lng REAL, adjacent_ids, created_at, updated_at)`
- `genders(id PK, slug UNIQUE, label, sort DEFAULT 0, active DEFAULT 1)`
- `traits(id PK, slug UNIQUE, label, emoji, sort DEFAULT 0)`
- `profiles(user_id PK, age, city_label, bio, gender_id, favorite_games, created_at, updated_at)`
- `user_intents(user_id, intent, PRIMARY KEY(user_id,intent))`
- `user_traits(user_id, trait_id, PRIMARY KEY(user_id,trait_id))`
- `preferences(user_id PK, age_min DEFAULT 18, age_max DEFAULT 99, distance_scope DEFAULT 'metro', who_to_meet_open DEFAULT 1, who_to_meet, created_at, updated_at)`
- `photos(id PK, user_id, path, thumb_path, moderation_state DEFAULT 'ok', blurhash, created_at, updated_at)`
- `blocks(id PK, blocker_id, blocked_id, created_at, updated_at, UNIQUE(blocker_id,blocked_id))`
- `reports(id PK, reporter_id, subject_id, reason, details, status DEFAULT 'open', created_at, updated_at)`
- `legal_holds(id PK, user_id, report_id, reason, status DEFAULT 'active', photo_keys, message_ids, purge_after, created_at, updated_at)`
- `feature_flags(key PK, value)`
- `allowlist_phones(phone_e164_hash PK, created_at, updated_at)`
- `prompt_bank(id PK, game_kind, locale DEFAULT 'en', payload, tags, active DEFAULT 1, nsfw_level DEFAULT 0)`
- `game_sessions(id PK, kind, mode, state DEFAULT 'pending', started_at, ended_at, config, current_round DEFAULT 0, answer_by, forfeit_after, created_at, updated_at)`
- `session_participants(session_id, user_id, seat, joined_at, last_poll_at, PRIMARY KEY(session_id,user_id))`
- `rounds(id PK, session_id, index, prompt_id, state DEFAULT 'open', answer_by, extra)`
- `round_answers(round_id, user_id, payload, submitted_at, PRIMARY KEY(round_id,user_id))`
- `queue_entries(id PK, user_id UNIQUE, metro_id, game_kind, enqueued_at, allow_practice DEFAULT 0, matched_session_id)`
- `pair_relationships(id PK, user_a, user_b, state DEFAULT 'open_play', a_action DEFAULT 'none', b_action DEFAULT 'none', pending_expires_at, cooldown_until, origin_session_id, created_at, updated_at, UNIQUE(user_a,user_b))`
- `matches(id PK, user_a, user_b, state DEFAULT 'mutual', origin_session_id, unmatched_by, matched_at, expires_at, created_at, updated_at, UNIQUE(user_a,user_b))`
- `chat_threads(id PK, match_id UNIQUE, state DEFAULT 'open', created_at, updated_at)`
- `messages(id PK, thread_id, sender_id, kind DEFAULT 'text', body, meta, created_at, updated_at)`
- `invites(id PK, from_user_id, to_user_id, game_kind, state DEFAULT 'pending', expires_at, created_at, updated_at)`
- `signal_events(id PK, user_id, session_id, kind, key, value, created_at, updated_at)`
- `compatibility_snapshots(id PK, session_id, scorer_version DEFAULT 'scorer_v0', user_a, user_b, score REAL, components, reasons, computed_at, UNIQUE(session_id,scorer_version))`
- `xp_events(id PK, user_id, kind, amount, created_at, updated_at)`
- `user_behavior_stats(user_id PK, dims, created_at, updated_at)`
- New (Go-only, replace Laravel internals): `sessions(id PK, user_id, created_at, expires_at, last_seen_at)`, `otp_codes(phone PK, code, expires_at)`

Indexes to recreate: `photos(user_id)`, `blocks(blocker_id)`, `blocks(blocked_id)`, `users(metro_id)`, `sessions.user_id` (app sessions), `queue_entries(game_kind, metro_id, matched_session_id)`, `rounds(session_id)`, `messages(thread_id)`, `signal_events(user_id)`, `xp_events(user_id)`.

## Review Focus

The spec is silent on these; each is a real way the port breaks. Each gets a test in the task that owns the code.

1. **Concurrent queue matching** — two waiters polling `GET /queue/status` at the same instant must not each mint a session to the other. Expect exactly one session, the loser sees `waiting`/`matched` to the same id.
2. **SQLite write contention** — concurrent answers/heartbeats must not return `SQLITE_BUSY` to the client. Expect no 5xx under parallel writes.
3. **Blocked-user visibility** — a block, or a "quiet pass", must not leak. Expect `GET /pairs` omits the row and `GET /pairs/:id` returns 404 for the non-passer.
4. **Photo input** — a 20 MB file, a `.gif` renamed `.jpg`, and `../` in the path must be rejected or contained. Expect 422/404 and no write outside the user's photo dir.
5. **Time handling** — an answer submitted at the deadline, and a server/client clock skew, must not double-fire a round. Expect monotonic round index and no skipped/replayed reveal.
6. **Payload equality** — reveal `same` compares JSON objects (`this_or_that` both choose left; 20Q same `optionId`). Expect key order and int/float drift not to change `same`.
7. **Session cookie** — cookie must be `HttpOnly`, `SameSite=Lax`, and survive a server restart (row persists). Expect auth to work after restart and to fail after logout.

---

### Task 1: Go module, config, and `/v1/healthz`

**Files:**
- Create: `apps/server/go.mod`, `apps/server/cmd/gamematch/main.go`, `apps/server/internal/config/config.go`, `apps/server/internal/httpapi/router.go`, `apps/server/internal/httpapi/respond.go`
- Test: `apps/server/internal/httpapi/router_test.go`

**Interfaces:**
- Produces: `config.Load() Config` with fields `Addr string`, `DataDir string`, `Debug bool`, `Env string`, `HouseUserID string`, `DevOTP string`; `httpapi.Router(cfg, deps) http.Handler`; `httpapi.WriteJSON(w, status, v)`, `httpapi.WriteError(w, status, code, msg)`.

- [ ] **Step 1: Install Go and init the module**
Run: `mise use -g go@latest && go version`
Then: `cd apps/server && go mod init gamematch` and add the deps from Tech Stack.

- [ ] **Step 2: Write the failing test**

```go
func TestHealthz(t *testing.T) {
	h := httpapi.Router(config.Config{Env: "test"}, httpapi.Deps{})
	req := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
	require.JSONEq(t, `{"ok":true}`, rec.Body.String())
}
```

- [ ] **Step 3: Run it to verify it fails**
Run: `cd apps/server && go test ./internal/httpapi/ -run TestHealthz`
Expected: FAIL (undefined `httpapi.Router`).

- [ ] **Step 4: Implement** `config.Load`, `Router` with a single `GET /v1/healthz` route (chi), `WriteJSON`/`WriteError`, and `main.go` that loads config and calls `http.ListenAndServe(cfg.Addr, Router(...))`.

- [ ] **Step 5: Run to verify it passes**
Run: `cd apps/server && go test ./... && go vet ./...`
Expected: PASS, no vet findings.

- [ ] **Step 6: Commit**
```bash
git add apps/server && git commit -m "Add Go service skeleton with health endpoint"
```

### Task 2: SQLite handle + migration runner + schema

**Files:**
- Create: `apps/server/internal/db/db.go`, `apps/server/internal/db/migrate.go`, `apps/server/internal/db/migrations/0001_init.sql`
- Test: `apps/server/internal/db/migrate_test.go`

**Interfaces:**
- Produces: `db.Open(path string) (*sqlx.DB, error)` (sets WAL, busy_timeout, foreign_keys, MaxOpenConns 1); `db.Migrate(ctx, *sqlx.DB) error`; `db.InTx(ctx, *sqlx.DB, func(*sqlx.Tx) error) error`.

- [ ] **Step 1: Write the failing test**

```go
func TestMigrateCreatesAllTables(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), d))
	var v int
	require.NoError(t, d.Get(&v, "PRAGMA user_version"))
	require.Equal(t, 1, v)
	for _, tbl := range []string{"users", "pair_relationships", "compatibility_snapshots", "sessions", "otp_codes"} {
		var n int
		require.NoError(t, d.Get(&n, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", tbl))
		require.Equal(t, 1, n)
	}
}
```

- [ ] **Step 2: Run to verify it fails**
Run: `cd apps/server && go test ./internal/db/ -run TestMigrateCreatesAllTables`
Expected: FAIL (undefined `db.Open`).

- [ ] **Step 3: Implement** the PRAGMAs, `embed.FS`-backed runner that applies `000N_*.sql` in order inside a transaction and bumps `PRAGMA user_version`, and `0001_init.sql` with every table/index from the Ported Schema.

- [ ] **Step 4: Run to verify it passes**
Run: `cd apps/server && go test ./internal/db/`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add SQLite schema and migration runner"
```

### Task 3: Capture Laravel golden responses

**Files:**
- Create: `apps/server/testdata/golden/README.md`, `apps/server/scripts/capture_golden.sh`, `apps/server/testdata/golden/*.json`
- Modify: `apps/api/tests/Feature/PrototypeTest.php` (only if a fixture account is missing)

**Interfaces:**
- Produces: `testdata/golden/<name>.json` fixtures keyed by a case name, plus a normalization rule documented in `testdata/golden/README.md` (replace `id`/`*Id` UUIDs with `"<uuid>"`, ISO timestamps with `"<ts>"`, absolute file URLs with the path only).

- [ ] **Step 1: Bring the reference up**
Run: `bash scripts/dev.sh` (MySQL + Laravel on `:8000`) and `cd apps/api && php artisan migrate:fresh --seed`.

- [ ] **Step 2: Write the capture script** that logs in as Alex (`/auth/otp/start`, `/auth/otp/verify`), then `curl -c/-b` the read endpoints (`/auth/session`, `/catalogs`, `/games`, `/legal`, `/me`, `/me/xp`, `/home`, `/pairs`, `/threads`) into `testdata/golden/`, normalizing per the README.

- [ ] **Step 3: Run it and verify the files exist and are valid JSON**
Run: `bash apps/server/scripts/capture_golden.sh && jq -e . apps/server/testdata/golden/*.json >/dev/null && echo ok`
Expected: `ok`.

- [ ] **Step 4: Commit**
```bash
git add apps/server/testdata apps/server/scripts && git commit -m "Capture Laravel responses as contract fixtures"
```

### Task 4: Session auth middleware + `GET /auth/session`

**Files:**
- Create: `apps/server/internal/store/store.go`, `apps/server/internal/store/auth.go`, `apps/server/internal/httpapi/middleware.go`
- Modify: `apps/server/internal/httpapi/router.go`, `apps/server/internal/httpapi/dto.go`
- Test: `apps/server/internal/store/auth_test.go`, `apps/server/internal/httpapi/auth_test.go`

**Interfaces:**
- Produces: `store.New(*sqlx.DB) *Store`; `Store.CreateSession(ctx, userID string, ttl time.Duration) (token string, err error)`; `Store.SessionUser(ctx, token string) (userID string, ok bool)`; `Store.DeleteSession(ctx, token string) error`; `httpapi.RequireUser(next http.Handler) http.Handler`; `httpapi.CurrentUser(r) store.User`; `httpapi.PublicMe(u store.User) PublicUser`.
- Consumes: `db.Open`, `db.Migrate` (Task 2).

- [ ] **Step 1: Write the failing tests**

```go
func TestSessionRoundTrip(t *testing.T) {
	s := newTestStore(t)
	tok, err := s.CreateSession(ctx, "u1", time.Hour)
	require.NoError(t, err)
	got, ok := s.SessionUser(ctx, tok)
	require.True(t, ok); require.Equal(t, "u1", got)
	require.NoError(t, s.DeleteSession(ctx, tok))
	_, ok = s.SessionUser(ctx, tok); require.False(t, ok)
}

func TestSessionEndpointUnauthenticated(t *testing.T) {
	r := routerForTest(t)
	rec := do(r, "GET", "/v1/auth/session", nil)
	require.Equal(t, 401, rec.Code)
	require.JSONEq(t, `{"error":{"code":"unauth","message":"Sign in"}}`, rec.Body.String())
}
```

- [ ] **Step 2: Run to verify they fail**
Run: `cd apps/server && go test ./internal/store/ ./internal/httpapi/ -run 'Session'`
Expected: FAIL.

- [ ] **Step 3: Implement** the `sessions` store, cookie read/write (`gm_session`, HttpOnly, SameSite=Lax, Secure iff `Env != "local"`), the auth middleware (401 `unauth` when absent), and the `GET /v1/auth/session` handler returning `PublicMe` and calling `touchPresence` (`last_seen_at=now`, `last_active_on=today`).

- [ ] **Step 4: Run to verify they pass**
Run: `cd apps/server && go test ./internal/store/ ./internal/httpapi/`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add cookie session auth"
```

### Task 5: OTP start/verify, demo OAuth, logout

**Files:**
- Create: `apps/server/internal/httpapi/auth.go`
- Modify: `apps/server/internal/store/auth.go`, `apps/server/internal/store/users.go`, `apps/server/internal/httpapi/router.go`
- Test: `apps/server/internal/httpapi/auth_test.go`

**Interfaces:**
- Produces: `Store.PutOTP(ctx, phone, code string, ttl time.Duration) error`; `Store.TakeOTP(ctx, phone string) (code string, ok bool)`; `Store.FindOrCreateUserByPhoneHash(ctx, hash string) (store.User, error)`; `Store.AllowlistHas(ctx, hash string) (bool, error)`; `store.HashPhone(phone string) string` (sha256 hex of the whitespace-stripped E.164).
- Consumes: `PublicMe`, session middleware (Task 4).

- [ ] **Step 1: Write the failing tests**

```go
func TestOTPVerifyLogsInAlex(t *testing.T) {
	r, s := routerWithSeed(t) // seeds Alex +15551111111
	require.Equal(t, 200, do(r, "POST", "/v1/auth/otp/start", jsonBody(`{"phone":"+15551111111"}`)).Code)
	rec := do(r, "POST", "/v1/auth/otp/verify", jsonBody(`{"phone":"+15551111111","code":"123456"}`))
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "Alex", jsonPath(rec, "user.name"))
	require.NotEmpty(t, sessionCookie(rec))
}

func TestOTPStartIsSilentForUnknownPhone(t *testing.T) {
	r, _ := routerWithSeed(t)
	rec := do(r, "POST", "/v1/auth/otp/start", jsonBody(`{"phone":"+19998887777"}`))
	require.Equal(t, 200, rec.Code)
	require.JSONEq(t, `{"ok":true}`, rec.Body.String()) // public_signup=false path
}
```

- [ ] **Step 2: Run to verify they fail**
Run: `cd apps/server && go test ./internal/httpapi/ -run 'OTP'`
Expected: FAIL.

- [ ] **Step 3: Implement** `otpStart` (normalize phone, `^\+?[0-9]{10,15}$`, allowlist gate when `auth.public_signup` is off, dev code vs random, 15m TTL, `devCode` only in debug), `otpVerify` (401 `invalid` on mismatch, create user + `user_private` + `preferences(who_to_meet_open=1)` on first login, set cookie, `touchPresence`), `oauthDemo(provider)` (422 `bad` for unknown provider; 501 when not debug and no client id; else find-or-create `<provider>-demo@gamematch.local`, login), and `logout`.

- [ ] **Step 4: Run to verify they pass**
Run: `cd apps/server && go test ./internal/httpapi/ -run 'OTP|OAuth|Logout'`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add phone OTP and demo sign-in"
```

### Task 6: `GET/PATCH /me` and the `PublicMe` DTO

**Files:**
- Create: `apps/server/internal/httpapi/me.go`, `apps/server/internal/store/users.go`
- Modify: `apps/server/internal/httpapi/dto.go`, `apps/server/internal/httpapi/router.go`
- Test: `apps/server/internal/httpapi/me_test.go`

**Interfaces:**
- Produces: `Store.UpdateProfile(ctx, userID string, p ProfilePatch) error`; `Store.MeView(ctx, userID string) (PublicUser, error)`; `httpapi.PublicUser` matching the golden `me` fixture (`id,name,onboardingStep,status,role,xp,level,incognito,metroId,profile,photos,intents,preferences`).
- Consumes: `PublicMe` (Task 4).

- [ ] **Step 1: Write the failing tests**

```go
func TestPatchMeOnboardingCompletesProfile(t *testing.T) {
	r, _ := routerWithSeed(t) ; loginAsAlex(t, r)
	rec := do(r, "PATCH", "/v1/me", jsonBody(`{"name":"Alex","dob":"1998-01-15","metro_id":"<LA>","bio":"hi","intents":["dating","gaming"],"age_attested":true,"onboarding_step":"done"}`))
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "active", jsonPath(rec, "user.status"))
	require.Equal(t, "done", jsonPath(rec, "user.onboardingStep"))
	require.Equal(t, []any{"dating", "gaming"}, jsonPath(rec, "user.intents"))
}

func TestPatchMeRejectsUnder18(t *testing.T) {
	r, _ := routerWithSeed(t) ; loginAsAlex(t, r)
	rec := do(r, "PATCH", "/v1/me", jsonBody(`{"dob":"2015-01-01"}`))
	require.Equal(t, 403, rec.Code)
	require.JSONEq(t, `{"error":{"code":"age","message":"You must be 18+"}}`, rec.Body.String())
}

func TestPatchMeReplacesIntentsAndTraits(t *testing.T) { /* set [dating], then [gaming] → exactly [gaming] */ }
```

- [ ] **Step 2: Run to verify they fail**
Run: `cd apps/server && go test ./internal/httpapi/ -run 'PatchMe'`
Expected: FAIL.

- [ ] **Step 3: Implement** `PATCH /me`: validate the exact field set from `ApiController::patchMe` (`name max40`, `dob`, `metro_id`, `bio max280`, `gender_id`, `age_attested`, `onboarding_step`, `intents`, `trait_ids`, `age_min/max 18..99`, `distance_scope metro|metro_and_adjacent`, `who_to_meet_open`, `who_to_meet`, `incognito`, `hidden`); age < 18 → 403; `onboarding_step=done` → `status=active`; upsert `profiles` (`age` from dob, `city_label` from metro); replace `user_intents`/`user_traits`; upsert `preferences`; return `PublicMe`. `GET /me` returns `{"user": PublicMe}`.

- [ ] **Step 4: Run to verify they pass**
Run: `cd apps/server && go test ./internal/httpapi/ -run 'PatchMe|GetMe'`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add profile read and update endpoints"
```

### Task 7: Photo upload and serving

**Files:**
- Create: `apps/server/internal/media/photos.go`, `apps/server/internal/httpapi/photos.go`, `apps/server/internal/store/photos.go`
- Modify: `apps/server/internal/httpapi/router.go`
- Test: `apps/server/internal/media/photos_test.go`, `apps/server/internal/httpapi/photos_test.go`

**Interfaces:**
- Produces: `media.Process(r io.Reader, maxEdge int) (full []byte, thumb []byte, err error)`; `Store.CreatePhoto(ctx, userID, path, thumbPath string) (store.Photo, error)`; `Store.PhotoByID(ctx, id string) (store.Photo, error)`; photo DTO `{id,url,state}` where `url` is `/v1/photos/<id>`.
- Consumes: `PublicMe` (Task 4).

- [ ] **Step 1: Write the failing tests**

```go
func TestUploadPhotoStoresFullAndThumb(t *testing.T) {
	full, thumb, err := media.Process(bytes.NewReader(pngBytes(t, 2000, 1200)), 1080)
	require.NoError(t, err)
	require.Equal(t, 1080, imageBounds(full).Dx())
	require.Equal(t, 720, imageBounds(full).Dy())
	require.Equal(t, 320, imageBounds(thumb).Dx())
}

func TestUploadRejectsGif(t *testing.T) { /* upload .gif → 422 bad_image */ }
func TestPhotoNotFoundForOtherUsersPhotoWhenBlocked(t *testing.T) { /* aborts 404 */ }
```

- [ ] **Step 2: Run to verify they fail**
Run: `cd apps/server && go test ./internal/media/ ./internal/httpapi/ -run 'Photo'`
Expected: FAIL.

- [ ] **Step 3: Implement** `media.Process` (decode jpeg/png/webp, downscale longest edge to `maxEdge` with `imaging`, re-encode JPEG q82; thumb 320px wide q75), and `POST /me/photos` (multipart field `photo`, 10 MB cap, mime allowlist, write `data/photos/<userID>/<uuid>.jpg` and `_t.jpg`, insert row `moderation_state='ok'`, return `{"photo":{"id","url"}}`), and `GET /photos/:id` (404 when the file or row is missing; 404 when blocked with the owner; `http.ServeFile`).

- [ ] **Step 4: Run to verify they pass**
Run: `cd apps/server && go test ./internal/media/ ./internal/httpapi/ -run 'Photo'`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add photo upload and serving"
```

### Task 8: Catalogs, games, legal

**Files:**
- Create: `apps/server/internal/httpapi/catalog.go`, `apps/server/internal/store/catalog.go`
- Modify: `apps/server/internal/httpapi/router.go`
- Test: `apps/server/internal/httpapi/catalog_test.go`

**Interfaces:**
- Produces: `Store.Catalogs(ctx) (Catalogs, error)`; handlers `GET /catalogs`, `GET /games`, `GET /legal`.

- [ ] **Step 1: Write the failing test** asserting `GET /v1/catalogs` returns `metros[{id,slug,label}]` with **no** `centroid_*`/`adjacent_ids`, `genders` active-only ordered by `sort`, `traits` ordered by `sort`, and the hard-coded 4 intents (`dating,friendship,gaming,socializing`); `GET /v1/games` returns the 3 ready kinds; `GET /v1/legal` returns the terms/privacy strings from `ApiController::legal`.
Run: `cd apps/server && go test ./internal/httpapi/ -run 'Catalog|Games|Legal'`
Expected: FAIL.

- [ ] **Step 2: Implement** the three handlers and the store queries.

- [ ] **Step 3: Run to verify it passes** — `go test ./internal/httpapi/ -run 'Catalog|Games|Legal'` → PASS.

- [ ] **Step 4: Commit**
```bash
git add apps/server && git commit -m "Add catalogs, games, and legal endpoints"
```

### Task 9: Seed data

**Files:**
- Create: `apps/server/internal/seed/seed.go`, `apps/server/cmd/gamematch/seed.go`
- Modify: `apps/server/cmd/gamematch/main.go` (add `seed` subcommand)
- Test: `apps/server/internal/seed/seed_test.go`

**Interfaces:**
- Produces: `seed.Run(ctx, *store.Store, cfg config.Config) error`.

- [ ] **Step 1: Write the failing test**

```go
func TestSeedCreatesDemoWorld(t *testing.T) {
	s := newSeededStore(t)
	alex, _ := s.UserByName(ctx, "Alex")
	require.Equal(t, "active", alex.Status)
	require.NotEmpty(t, alex.MetroID)
	staff, _ := s.UserByName(ctx, "Staff")
	require.Equal(t, "admin", staff.Role)
	require.Equal(t, 10, count(t, s.DB, "SELECT count(*) FROM prompt_bank WHERE game_kind='this_or_that'"))
	require.Equal(t, 12, count(t, s.DB, "SELECT count(*) FROM prompt_bank"))           // 6 20Q + 6 GMA
	require.True(t, flagOn(t, s, "auth.public_signup"))
	require.Equal(t, 3, count(t, s.DB, "SELECT count(*) FROM allowlist_phones"))
	house, _ := s.UserByID(ctx, cfg.HouseUserID)
	require.Equal(t, "House", house.Name)
}
```

- [ ] **Step 2: Run to verify it fails** — `go test ./internal/seed/` → FAIL.

- [ ] **Step 3: Implement** the exact seeder: 2 metros (LA, NY), 4 genders, 10 traits, 10 this-or-that prompts, 6 20Q + 6 GMA from the same 6 questions, 3 feature flags, the House user, Alex/Jordan (`demoUser`: profile, preferences age 21–40, intents, first 6 traits, `user_private`, xp 40), Staff (admin), 3 allowlisted phones.

- [ ] **Step 4: Run to verify it passes** — `go test ./internal/seed/` → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add seed data and seed subcommand"
```

### Task 10: Queue + matcher hard filters

**Files:**
- Create: `apps/server/internal/game/policy.go`, `apps/server/internal/game/matcher.go`, `apps/server/internal/store/queue.go`, `apps/server/internal/httpapi/queue.go`
- Modify: `apps/server/internal/httpapi/router.go`
- Test: `apps/server/internal/game/policy_test.go`, `apps/server/internal/game/matcher_test.go`, `apps/server/internal/httpapi/queue_test.go`

**Interfaces:**
- Produces: `policy.OrderedPair(a,b string) (string,string)`; `policy.BlockedEitherWay(ctx, s, a, b) bool`; `policy.IntentsOK(a,b []string) bool`; `policy.AgeOK(a,b store.User) bool`; `policy.GenderOK(viewer, other store.User) bool`; `Matcher.Enqueue(ctx, userID, kind string, allowPractice bool) error`; `Matcher.Tick(ctx, userID string) (QueueStatus, error)`; `Matcher.Leave(ctx, userID string) error`.
- Consumes: `Engine.StartSession` (Task 11) for `queue_1v1`/`practice`.

- [ ] **Step 1: Write the failing tests**

```go
func TestIntentsOK(t *testing.T) {
	require.True(t, policy.IntentsOK([]string{"dating", "gaming"}, []string{"dating"}))
	require.False(t, policy.IntentsOK([]string{"dating"}, []string{"friendship"}))
	require.False(t, policy.IntentsOK([]string{"dating"}, []string{"gaming"}))
	require.True(t, policy.IntentsOK([]string{"friendship"}, []string{"gaming"}))
	require.False(t, policy.IntentsOK(nil, []string{"gaming"}))
}

func TestTickPairsAlexAndJordan(t *testing.T) {
	m, s := seededMatcher(t)
	require.NoError(t, m.Enqueue(ctx, alexID, "this_or_that", false))
	require.NoError(t, m.Enqueue(ctx, jordanID, "this_or_that", false))
	st, err := m.Tick(ctx, alexID)
	require.NoError(t, err)
	require.Equal(t, "matched", st.State)
	session := mustSession(t, s, st.SessionID)
	require.Equal(t, "queue_1v1", session.Mode)
	require.Equal(t, 2, countParticipants(t, s, session.ID))
}

func TestTickWaitsWithoutMatch(t *testing.T) { /* only Alex queued → state=waiting, canPractice=false at t=0 */ }
func TestConcurrentTickMintsOneSession(t *testing.T) { /* both tick in goroutines → same sessionId */ }
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/game/ ./internal/httpapi/ -run 'Intents|Tick|Queue'` → FAIL.

- [ ] **Step 3: Implement** `policy` (port `Safety` verbatim, including `intentsOk` empty→false, `ageOk` requiring both profiles+prefs, `genderOk` open→true else membership), and `Matcher` (delete-then-insert queue row; `Tick`: idle/matched/waiting; candidate query filtered by game kind, different user, unmatched, same metro, ordered by `enqueued_at`, capped 20, then hard filters capped 5, pick first; practice after 30s when `allow_practice`; metro `adjacent_ids` expansion after 32s; waiting payload `{state,gameKind,positionHint:"searching",estimatedWaitSec:45,waitedSec,canPractice}`). `POST /queue` returns `Tick`; `GET /queue/status` returns `Tick`; `DELETE /queue` returns `{ok:true}`; incognito → 409 `incognito`.

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add queue matching with hard filters"
```

### Task 11: Game engine, `this_or_that` lifecycle

**Files:**
- Create: `apps/server/internal/game/engine.go`, `apps/server/internal/store/sessions.go`, `apps/server/internal/httpapi/sessions.go`
- Modify: `apps/server/internal/httpapi/router.go`, `apps/server/internal/httpapi/dto.go`
- Test: `apps/server/internal/game/engine_test.go`, `apps/server/internal/httpapi/sessions_test.go`

**Interfaces:**
- Produces: `Engine.StartSession(ctx, kind, mode string, userIDs []string) (store.Session, error)`; `Engine.Join(ctx, sessionID, userID string) error`; `Engine.Heartbeat(ctx, sessionID, userID string) error`; `Engine.Answer(ctx, sessionID, userID string, payload json.RawMessage) error`; `Engine.Advance(ctx, sessionID string) error`; `Engine.View(ctx, sessionID, userID string) (SessionView, error)`; `Engine.LevelForXP(xp int) int`.
- Consumes: `policy`, `Scorer` (Task 13).

- [ ] **Step 1: Write the failing tests**

```go
func TestSessionRoundLifecycle(t *testing.T) {
	e := newEngine(t) ; ses := startTorThat(t, e, alexID, jordanID)
	require.NoError(t, e.Join(ctx, ses.ID, alexID))
	require.NoError(t, e.Join(ctx, ses.ID, jordanID))
	require.Equal(t, "countdown", mustSession(t, e.Store, ses.ID).State)
	forceStarted(t, e, ses.ID)                     // simulate countdown elapsed
	require.NoError(t, e.Advance(ctx, ses.ID))
	require.Equal(t, "in_round", mustSession(t, e.Store, ses.ID).State)
	require.NoError(t, e.Answer(ctx, ses.ID, alexID, json.RawMessage(`{"choice":"left"}`)))
	require.NoError(t, e.Answer(ctx, ses.ID, jordanID, json.RawMessage(`{"choice":"left"}`)))
	require.Equal(t, "reveal_round", mustSession(t, e.Store, ses.ID).State)
	v, err := e.View(ctx, ses.ID, alexID)
	require.NoError(t, err)
	require.True(t, v.Reveal.Same)
}

func TestPendingSessionCancelsAfter30s(t *testing.T) { /* backdate created_at → cancelled */ }
func TestStaleParticipantForfeits(t *testing.T) { /* backdate last_poll_at 16s → forfeit */ }
func TestLevelForXP(t *testing.T) {
	require.Equal(t, 1, game.LevelForXP(0)); require.Equal(t, 2, game.LevelForXP(100)); require.Equal(t, 3, game.LevelForXP(300))
}
func TestAnswerAfterDeadlineDoesNotAdvance(t *testing.T) { /* clock at deadline boundary → no double round */ }
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/game/ -run 'Session|Pending|Stale|Level|Deadline'` → FAIL.

- [ ] **Step 3: Implement** `StartSession` (rounds per kind 8/6/6, `config={rounds,countdown_ms:3000,reveal_ms:4000}`, participants with seats) and the `Advance` state machine ported line-for-line from `GameEngine::advance` (its three blocks: stale→forfeit, pending-30s→cancel, countdown→open round 1, in_round both-or-timeout→reveal, reveal-deadline→next round-or-complete), plus `openRound` (random active prompt for kind, `answer_by = now + (kind=="this_or_that" ? 8s : 12s)`, GMA `extra={answerer,guesser}`), `join` (practice needs 1, else 2), `heartbeat` (update `last_poll_at` then `Advance`), `answer` (Advance first; 409 `closed` unless `in_round`; upsert `round_answers`; practice inserts House `{choice:"left"}`/`{optionId: options[0]}`; Advance again), and `View` (seat, opponent incl. House, `RoundView{index,total,kind,prompt,yourRole,deadline{answerBy,serverNow},youSubmitted,opponentSubmitted}`, `reveal{you,opponent,same}`, `completed`, `serverNow`). All transitions in `BEGIN IMMEDIATE` transactions.

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add game engine and this-or-that lifecycle"
```

### Task 12: `twenty_questions` and `guess_my_answer`

**Files:**
- Modify: `apps/server/internal/game/engine.go`, `apps/server/internal/game/engine_test.go`

**Interfaces:**
- Consumes: Task 11 `Engine`; produces no new exported API.

- [ ] **Step 1: Write the failing tests**

```go
func TestTwentyQuestionsSameOption(t *testing.T) { /* both answer optionId "a" → reveal.Same true */ }
func TestGuessMyAnswerAssignsRoles(t *testing.T) {
	/* seat 0 answerer on even rounds; View(a).Round.YourRole == "answerer", View(b) == "guesser" */
}
func TestGuessMyAnswerSameWhenGuessMatchesSecret(t *testing.T) { /* answerer "b", guesser "b" → same */ }
func TestGuessMyAnswerMismatch(t *testing.T) { /* answerer "b", guesser "c" → same false */ }
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/game/ -run 'Questions|GuessMyAnswer'` → FAIL.

- [ ] **Step 3: Implement** the shared 4-option path (payload `{question,options[{id,label,tags}]}`, submit `{optionId}`), and GMA role assignment in `openRound` (`answerer = seats[index % len(seats)]`, `guesser = the other`) with `View` reporting `yourRole`; `same` for GMA compares `answerer.optionId == guesser.optionId`. Keep single-phase (matches the prototype; the doc's two-phase flow is Task 22).

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add 20-questions and guess-my-answer rounds"
```

### Task 13: `scorer_v0`, signals, XP, pair creation on completion

**Files:**
- Create: `apps/server/internal/game/scorer.go`, `apps/server/internal/store/xp.go`
- Modify: `apps/server/internal/game/engine.go` (`complete`), `apps/server/internal/game/engine_test.go`
- Test: `apps/server/internal/game/scorer_test.go`

**Interfaces:**
- Produces: `Scorer.ScoreSession(ctx, sessionID string) (*Snapshot, error)`; `Store.CreateSnapshot(...)`; `Store.AddXP(ctx, userID, kind string, amount int) error`.
- Consumes: `policy.OrderedPair`, `Engine.LevelForXP`.

- [ ] **Step 1: Write the failing tests**

```go
func TestScoreSessionWeights(t *testing.T) {
	/* traits jaccard 1.0, intents jaccard 1.0, behavior 1.0, same metro:
	   score = .3125*1 + .25*1 + .1875*1 + .1875*1 + .0625*1 = 1.0; percent nil until n>=6 */
}
func TestScoreSkipPractice(t *testing.T) { /* practice → nil snapshot, no signals */ }
func TestSignalsWrittenPerAnsweredRound(t *testing.T) { /* 8 rounds answered → 16 signal_events (8 per user) */ }
func TestCompleteAwardsXPAndCreatesPair(t *testing.T) {
	/* both reach 50 XP; pair_relationships row state=open_play, a_action=b_action="none", origin_session_id set */
}
func TestCompleteRespectsCooldown(t *testing.T) { /* closed pair with future cooldown stays closed */ }
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/game/ -run 'Score|Signal|Complete'` → FAIL.

- [ ] **Step 3: Implement** `ScoreSession` porting `Scorer::scoreSession` exactly (jaccard of traits and intents, behavior = same/n over rounds with 2 answers, location 0.6 on equal metro, weights `.3125/.25/.1875/.1875/.0625`, clamp, reason thresholds `traits>=.3`/`behavior>=.5`/`intents>=.5`/`loc>0` sliced to 3 with fallback `["Still getting to know you"]`, `percent` only when `n>=6`), inserting `signal_events` per answered round (`kind="choice"`, `key="round.<idx>"`, `value=payload`) and one `compatibility_snapshots` row with `scorer_version="scorer_v0"`. Implement `complete`: `state=scoring` → score → `state=completed`, `ended_at=now`; XP 50 (5 practice, House excluded) with `level` recomputed; upsert `pair_relationships` to `open_play` unless an active cooldown on `closed|unmatched|blocked`.

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add compatibility scoring, signals, and XP"
```

### Task 14: Session endpoints + background advancer

**Files:**
- Modify: `apps/server/internal/httpapi/sessions.go`, `apps/server/internal/httpapi/router.go`, `apps/server/cmd/gamematch/main.go`
- Test: `apps/server/internal/httpapi/sessions_test.go`

**Interfaces:**
- Produces: routes `POST /sessions/:id/join`, `GET /sessions/:id`, `POST /sessions/:id/answer`, `POST /sessions/:id/leave`, `POST /sessions/:id/rematch`; `cmd` starts `Engine.RunTicker(ctx, 5*time.Second)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestSessionShowCountsAsHeartbeat(t *testing.T) { /* GET advances last_poll_at */ }
func TestSessionJoinReturnsViewForParticipant(t *testing.T) { }
func TestSessionJoinForbiddenForStranger(t *testing.T) { /* 403 forbidden */ }
func TestSessionLeaveForfeits(t *testing.T) { /* POST leave → state forfeit */ }
func TestRematchCreatesInvite(t *testing.T) { /* rematch → invites row to the opponent, gameKind = session kind */ }
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/httpapi/ -run 'Session'` → FAIL.

- [ ] **Step 3: Implement** the handlers (`join`→`Join`+`View`; `show`→`Heartbeat`+`View`; `answer`→`Heartbeat`+`Answer`+`View`; `leave`→`forfeit` unless already terminal; `rematch`→404 for non-participants or practice, else delegate to invite creation for the other participant) and the 5s ticker that advances all non-terminal sessions.

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add session endpoints and background advancer"
```

### Task 15: `GET /home`

**Files:**
- Create: `apps/server/internal/httpapi/home.go`
- Test: `apps/server/internal/httpapi/home_test.go`

**Interfaces:**
- Produces: `GET /home` → `{"queueDepth":int,"invites":[...],"pendingPairs":int}` (golden-matched to Laravel, which returns the short shape `{queueDepth,invites,pendingPairs}`).

- [ ] **Step 1: Write the failing test** — seeded world: `queueDepth` counts unmatched `this_or_that` waiters; one pending invite for Alex appears in `invites` with `other` card and `fromMe`; `pendingPairs` counts `pair_relationships` in `pending` involving the user. Run → FAIL.

- [ ] **Step 2: Implement** the handler and the `inviteItem`/`card` DTO builders (`card = {id,name,age,bio,photoUrl}` using the first `moderation_state='ok'` photo, `photoUrl` = `/v1/photos/<id>`).

- [ ] **Step 3: Run to verify it passes** → PASS.

- [ ] **Step 4: Commit**
```bash
git add apps/server && git commit -m "Add home summary endpoint"
```

### Task 16: Pairs — list, show, connect/pass, unmatch, mutual

**Files:**
- Create: `apps/server/internal/httpapi/pairs.go`, `apps/server/internal/store/pairs.go`
- Test: `apps/server/internal/httpapi/pairs_test.go`

**Interfaces:**
- Produces: `GET /pairs`, `GET /pairs/:id`, `POST /pairs/:id/connect`, `POST /pairs/:id/unmatch`; `Store.Mutual(ctx, pairID) error` (match upsert + thread + icebreaker + 20 XP each).
- Consumes: `policy.BlockedEitherWay`, `Store.AddXP`.

- [ ] **Step 1: Write the failing tests**

```go
func TestConnectMakesMutualAndOpensThread(t *testing.T) {
	/* both connect → pair.state mutual, matches row mutual, chat_threads open,
	   one icebreaker message with meta.actions, +20 XP each */
}
func TestPassClosesWithCooldown(t *testing.T) { /* pass → closed, cooldown_until ~now+14d */ }
func TestClosedHidesFromNonPasser(t *testing.T) { /* passer sees row with state closed; other gets 404 on /pairs/:id and no row in /pairs */ }
func TestBlockedPairIsHiddenAnd404s(t *testing.T) { /* after POST /blocks → /pairs omits; /pairs/:id 404 */ }
func TestConnectOnClosedIs409(t *testing.T) { /* code=closed */ }
func TestUnmatchLocksThread(t *testing.T) { /* state unmatched, cooldown 14d, thread locked */ }
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/httpapi/ -run 'Pair|Connect|Pass|Mutual|Unmatch'` → FAIL.

- [ ] **Step 3: Implement** the list (skip blocked, skip `closed` unless viewer passed; `yourAction`, `theyConnected`, `inviteEligible = state in {open_play,mutual,expired,unmatched} && cooldown past`, `other` card), show (same hide predicate → 404), connect/pass (`connect|pass` else 422; 404 for non-participants; 409 `{code:"closed"}` unless `open_play|pending`; set the actor's action then: any pass → `closed` + 14d cooldown; both connect → mutual; else `pending` + 72h window), mutual (upsert `matches` a<b, reopen/create thread — system divider "You matched again" when reopening — insert icebreaker with `meta.actions=["me","them","compete","play_again"]`, +20 XP each), and unmatch (pair/matches → `unmatched`, 14d cooldown, thread `locked`).

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add pairs, connect/pass, and mutual matching"
```

### Task 17: Threads and messages

**Files:**
- Create: `apps/server/internal/httpapi/threads.go`, `apps/server/internal/store/pairs.go` (messages/threads)
- Test: `apps/server/internal/httpapi/threads_test.go`

**Interfaces:**
- Produces: `GET /threads`, `GET /threads/:id/messages`, `POST /threads/:id/messages`; `Store.SendMessage(ctx, threadID, senderID, kind, body string) (Message, error)`; `Store.Messages(ctx, threadID string) ([]Message, error)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestThreadsListsMutualMatches(t *testing.T) { /* includes threadId, matchId, state, other card, last */ }
func TestMessagesRequireParticipant(t *testing.T) { /* stranger → 404 */ }
func TestSendMessageGivesFirstMessageXP(t *testing.T) { /* first text → +10 XP, second → no XP */ }
func TestSendMessageRejectsEmpty(t *testing.T) { /* 422 code=empty */ }
func TestSendMessageWhenLocked(t *testing.T) { /* 403 code=locked */ }
func TestIcebreakerBodyAllowlist(t *testing.T) { /* kind=icebreaker with body not in the 4 allowed strings is stored as text */
}
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/httpapi/ -run 'Thread|Message|Icebreaker'` → FAIL.

- [ ] **Step 3: Implement** threads (state=`mutual` matches, skip blocked, latest message as `last {body,kind,at}`), messages (`state` + `items` ordered by `created_at` with `{id,senderId,kind,body,meta,createdAt}`), and send (`assertNotBlocked`; 403 `locked` unless thread open; trim; 422 `empty`; allowed icebreaker bodies `["I'll pick","You pick","Let's compete","Play again"]` else downgrade to `text`; first-ever `text` message → +10 XP; clear `matches.expires_at`).

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add chat threads and messages"
```

### Task 18: Invites

**Files:**
- Create: `apps/server/internal/httpapi/invites.go`
- Test: `apps/server/internal/httpapi/invites_test.go`

**Interfaces:**
- Produces: `POST /invites`, `POST /invites/:id/accept`, `POST /invites/:id/decline`.
- Consumes: `Engine.StartSession`, `policy`.

- [ ] **Step 1: Write the failing tests**

```go
func TestInviteRequiresExistingPair(t *testing.T) { /* no pair → 409 code=unknown */ }
func TestInviteBlockedByCooldown(t *testing.T) { /* pair cooldown future → 409 code=cooldown */ }
func TestInviteHonorsBlock(t *testing.T) { /* blocked → 404 */ }
func TestInviteAcceptStartsSession(t *testing.T) { /* recipient accept → session mode=invite, 2 participants */ }
func TestInviteDeclineOnlyByRecipient(t *testing.T) { /* sender decline → 404; recipient → state declined */ }
func TestInviteExpiresAfter2m(t *testing.T) { /* backdate expires_at → not in home invites */ }
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/httpapi/ -run 'Invite'` → FAIL.

- [ ] **Step 3: Implement** create (assertNotBlocked; require an existing `pair_relationships` row for the ordered pair; 409 `unknown` when absent; 409 `cooldown` when `cooldown_until` future; insert invite `pending`, `expires_at=now+2m`; return `inviteItem`), accept (404 unless recipient and pending; `accepted`; start `invite` session; `{sessionId, gameKind}`), decline (404 unless recipient; `declined`).

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add game invites"
```

### Task 19: Blocks, reports, legal holds, account deletion

**Files:**
- Create: `apps/server/internal/httpapi/safety.go`, `apps/server/internal/store/safety.go`
- Modify: `apps/server/internal/httpapi/me.go` (`DELETE /me`)
- Test: `apps/server/internal/httpapi/safety_test.go`

**Interfaces:**
- Produces: `POST /blocks`, `DELETE /blocks/:id`, `POST /reports`, `DELETE /me`; `Store.CreateLegalHold(...)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestBlockHidesPairAndLocksMatch(t *testing.T) { /* /pairs count 0; matches state blocked; thread locked */ }
func TestUnblockReopensAsUnmatchedWithCooldown(t *testing.T) { }
func TestReportCsamHidesUserAndCreatesHold(t *testing.T) { /* subject status hidden; legal_holds active with purge_after */ }
func TestDeleteMeBlockedByLegalHold(t *testing.T) { /* 409 code=legal_hold */ }
func TestDeleteMeAnonymizesAndRemovesPhotos(t *testing.T) { /* status deleted, name "Deleted", email nil, files gone, logged out */ }
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/httpapi/ -run 'Block|Report|Delete'` → FAIL.

- [ ] **Step 3: Implement** block (insert `blocks`, pair→`blocked`, match→`blocked`, thread→`locked`), unblock (delete row; if not blocked either way: pair→`unmatched` + 14d cooldown, match→`unmatched`), report (insert; `csam|underage` → subject `hidden` + active `legal_hold` `purge_after=now+90d`), and delete-me (409 when an active hold exists; unlink photo files; `status=deleted`, `name="Deleted"`, `email=nil`; drop the session).

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add blocks, reports, and account deletion"
```

### Task 20: Staff tools, XP summary, staff allowlist

**Files:**
- Create: `apps/server/internal/httpapi/staff.go`
- Modify: `apps/server/internal/httpapi/me.go` (`GET /me/xp`)
- Test: `apps/server/internal/httpapi/staff_test.go`

**Interfaces:**
- Produces: `GET/POST /staff/allowlist`, `GET /internal/mod/reports`, `POST /internal/force-pair`, `GET /me/xp`.

- [ ] **Step 1: Write the failing tests**

```go
func TestAllowlistRequiresAdmin(t *testing.T) { /* Alex → 403; Staff → 200 */ }
func TestAllowlistPostPhoneAndToggleSignup(t *testing.T) { /* count increments; flag flips */ }
func TestModReportsRequiresStaffRole(t *testing.T) { /* Alex 403; Staff items */ }
func TestForcePairNeedsFlagAndRole(t *testing.T) { /* flag off → 404; on+admin → sessionId */ }
func TestMeXpBadges(t *testing.T) { /* xp>=100 → regular; session_complete → first-game; mutual → spark; first_message → icebreaker */ }
```

- [ ] **Step 2: Run to verify they fail** — `go test ./internal/httpapi/ -run 'Allowlist|ModReports|ForcePair|MeXp'` → FAIL.

- [ ] **Step 3: Implement** allowlist (admin only; POST phone → hash insert; `public_signup` present → upsert `feature_flags['auth.public_signup']`; return `{publicSignup,count}`), mod reports (role mod|admin; newest 100), force-pair (`role in {mod,admin}` **and** flag `staff.force_pair` else 404; start `invite` session), and `/me/xp` (latest 20 `xp_events`, badges per the same rules).

- [ ] **Step 4: Run to verify they pass** — same command → PASS.

- [ ] **Step 5: Commit**
```bash
git add apps/server && git commit -m "Add staff tools and XP summary"
```

### Task 21: Contract parity against the golden fixtures

**Files:**
- Create: `apps/server/internal/httpapi/contract_test.go`, `apps/server/internal/httpapi/normalize.go`
- Modify: `apps/server/scripts/capture_golden.sh` (emit the Go-side comparison harness)

**Interfaces:**
- Consumes: all handlers; `testdata/golden/*.json`.

- [ ] **Step 1: Write the failing test** — for each golden file: seed the Go DB with the same fixture IDs, log in as Alex, hit the matching endpoint, normalize both bodies (UUIDs → `"<uuid>"`, timestamps → `"<ts>"`, photo paths), and `require.JSONEq`. Run → FAIL until shapes match.

- [ ] **Step 2: Fix every drift** the comparison surfaces (field names, null vs missing, ordering). Do not change the fixtures to match Go; change Go to match Laravel.

- [ ] **Step 3: Run to verify it passes**
Run: `cd apps/server && go test ./... -race`
Expected: PASS with `-race`.

- [ ] **Step 4: Commit**
```bash
git add apps/server && git commit -m "Assert API parity against captured responses"
```

### Task 22 (optional follow-ups): doc-only features

**Files:** as needed per feature.

Skip unless the user asks. Each is its own plan, not part of parity: two-phase Guess My Answer (`answer` then `guess`, 8s/12s), 20s 20-Questions timer, rate limits (OTP 3/phone/15m, queue 5/min, invites 10/h, messages 20/min, reports 10/day, session poll 2/s), `Idempotency-Key` handling, `user_behavior_stats` materialization, `GET /matches`, `GET /me/blocks`, `GET /me/export`, Web Push, geolocation metro mapping, Turnstile CAPTCHA.

### Task 23: Port dev tooling and cut the frontend over

**Files:**
- Modify: `scripts/dev.sh`, `package.json` (root), `docker-compose.yml`, `.gitignore`, `README.md`
- Test: `apps/server/scripts/smoke.sh`

**Interfaces:**
- Consumes: the whole Go binary.

- [ ] **Step 1: Write the failing smoke test** `apps/server/scripts/smoke.sh`: start `apps/server` on `:8000`, `curl /v1/healthz`, OTP login as Alex, `GET /v1/home`, assert 200s; exit non-zero otherwise. Run it against the not-yet-wired dev script → FAIL.

- [ ] **Step 2: Rewire** `scripts/dev.sh` to `go run ./apps/server/cmd/gamematch serve` (no MySQL init), drop the MySQL service from `docker-compose.yml` (or delete the file and keep it in history), change root `package.json`'s `api` script to the Go run, and add `apps/server/data/` to `.gitignore`.

- [ ] **Step 3: Verify end to end**
Run: `bash apps/server/scripts/smoke.sh` and, in another shell, `pnpm dev` then load `http://localhost:5173` and complete: welcome → OTP → onboarding → queue → a full `this_or_that` game → connect → chat.
Expected: no console errors; the PWA still builds with `pnpm --filter web build`.

- [ ] **Step 4: Commit**
```bash
git add -A && git commit -m "Run the app on the Go service in dev"
```

### Task 24: Remove Laravel and update docs

**Files:**
- Delete: `apps/api/**` (Laravel), `Dockerfile`/`docker-compose.yml` if now unused
- Modify: `README.md`, `docs/design-docs-and-wireframes.md` (Tech stack + Hosting sections), `apps/server/testdata/golden/README.md`

**Interfaces:** none.

- [ ] **Step 1: Move the capture tooling** so it survives (keep `apps/server/testdata/golden/*` and `scripts/capture_golden.sh` with a note that it requires the historical Laravel app at a tagged commit).

- [ ] **Step 2: Delete Laravel**
Run: `git rm -r apps/api && git commit -m "Remove the Laravel service"`
(The code remains reachable at its prior commit for the capture script.)

- [ ] **Step 3: Update docs** — README stack line ("React front end, Go + SQLite backend"), local-run commands, demo accounts; design doc: replace the PHP/MySQL/Hostinger rows with Go/SQLite/VPS and note the deployment decision from Design Decision 8.

- [ ] **Step 4: Verify**
Run: `cd apps/server && go build ./... && go test ./... && bash scripts/smoke.sh` (repo root) and `pnpm --filter web build`.
Expected: all pass; no references to `apps/api` remain (`grep -rn "apps/api" --exclude-dir=.git .`).

- [ ] **Step 5: Commit**
```bash
git add -A && git commit -m "Remove the PHP backend and document the Go stack"
```

---

## Self-Review

- **Spec coverage:** every route in `apps/api/routes/api.php` maps to a task (auth→5, me/photos→6/7, catalogs/games/legal→8, home→15, queue→10, sessions→11/12/14, pairs→16, threads→17, invites→18, blocks/reports/delete→19, staff/internal→20, me/xp→20). Schema in the Ported Schema section matches `2026_08_26_100000_create_gamematch_tables.php`. The seed task mirrors `DatabaseSeeder`.
- **Deliberate omissions:** doc-only features (two-phase GMA, rate limits, idempotency, behavior stats, push, `/matches`, `/me/blocks`, `/me/export`) are listed in Task 22 rather than implemented, because the running prototype does not have them; parity is the goal.
- **Type consistency:** `Engine` methods, `Store` methods, `policy.*`, and DTO field names are used identically across tasks; DTO shape is pinned by the golden fixtures (Task 21) rather than re-declared per task.
- **Review Focus tests:** concurrency→Task 10, SQLite contention→Task 11 (`-race`, `BEGIN IMMEDIATE`), visibility→Task 16, photo input→Task 7, time handling→Task 11, payload equality→Tasks 11–12, cookie→Task 4.
- **Open decisions to confirm before implementation:** Design Decisions 1–8, especially the `apps/server` vs `apps/api` naming, porting the code (not the doc) for GMA/20Q timers, and the deployment target (Design Decision 8).
