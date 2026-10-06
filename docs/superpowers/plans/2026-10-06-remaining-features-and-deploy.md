# Remaining Features + Caddy Deploy — Implementation Plan

> **Status:** in progress (2026-10-06). Executed on branch `feat/remaining-features-and-deploy`.

**Goal:** Close every gap between the prototype `apps/server` (Go + SQLite) and the
normative v1 spec in `docs/design-docs-and-wireframes.md`, and make the app
deployable on a single Caddy-fronted host.

**Architecture:** unchanged — one Go binary owning one SQLite file. New work is
additive: SQLite migrations, `internal/*` packages, `/v1` handlers, and a
`deploy/` directory (Caddyfile + systemd unit + env template).

**Spec:** `docs/design-docs-and-wireframes.md`. Section references below are to
that file.

## Global Constraints

- Keep the existing `/v1` contract; new endpoints are additive.
- Errors stay `{"error":{"code":"…","message":"…"}}`; 429 adds `Retry-After`.
- Everything third-party is **config-gated and inert when unconfigured**: no
  Twilio, Google, Apple, Cloudflare, or VAPID keys means the code path logs and
  degrades instead of failing. Tests use fakes; **no network calls in tests**.
- Never log or store raw coordinates, geohash, DOB, or phone ciphertext.
- `csam`/`underage` reports are never rate-limit-dropped (§Reports).
- Migrations are append-only files in `internal/db/migrations` (never edit 0001).

## Design Decisions

1. **Deploy target is this host, fronted by Caddy.** `deploy/Caddyfile` serves
   `apps/web/dist` and reverse-proxies `/v1/*` to the Go service. TLS is Caddy's
   job: with `GAMEMATCH_SITE` set to a real domain it uses ACME; left unset it
   serves plain HTTP on `:80` for local use. Caddy and the service are the
   operator's to install; this plan ships config + scripts, not system changes.
2. **Rate limits live in SQLite, not memory** (`rate_limits` counters keyed by
   bucket). A restart must not reset an abuse window.
3. **Idempotency keys are stored with the response** (`idempotency_keys`), so a
   retry replays the original body instead of double-acting. Keyed by
   `(user_id, key)`; a mismatched request hash is a 409.
4. **Scorer follows the spec, not the prototype.** This is the one behaviour
   change: `P`/`L`/`I` sets from trait axis + picked option tags, Jaccard
   empty∪empty = 0, `behavior_sim` from `user_behavior_stats` with Laplace
   smoothing, location 1.0/0.5/0 for same/adjacent/other metro, percent hidden
   below 8 tagged answers, and the §Reason copy table with its ranking. Weights
   stay `0.3125/0.25/0.1875/0.1875/0.0625`.
5. **Guess My Answer becomes two-phase** (8s answer, 12s guess) and 20 Questions
   moves to a 20s timer, per §Game protocols. `this_or_that` stays 8s.
6. **Push is opt-in and backend-complete.** Subscriptions persist; sends are
   fire-and-forget and silent when VAPID keys are absent.
7. **`GET /me/export` is synchronous JSON** (v1.1 async job is not needed at this size).

## Phase 1 — Caddy deploy

**Files:** `deploy/Caddyfile`, `deploy/gamematch.service`, `deploy/gamematch.env.example`,
`scripts/deploy.sh`, `docs/runbooks/deploy.md`, `internal/config/dotenv.go`

- `config.Load` reads an optional `.env` (path from `ENV_FILE`, default
  `./.env`, then `DATA_DIR/../.env`) before the environment, without overriding
  real env vars.
- `deploy/Caddyfile`: `{$GAMEMATCH_SITE:localhost}` → `handle /v1/*` reverse_proxy
  `127.0.0.1:8000`, `handle` static `apps/web/dist` with SPA fallback, gzip, and
  `Cache-Control` for hashed assets.
- `scripts/deploy.sh`: `pnpm --filter web build`, `go build -o apps/server/bin/gamematch`,
  then run migrations/seed smoke and print the reload command.
- Unit file runs the binary with `EnvironmentFile=`, `Restart=always`, and a
  `WorkingDirectory` under the data dir.
- Tests: `internal/config` dotenv parsing (file wins over defaults, env wins over file).

## Phase 2 — Rate limits, Turnstile, SMS, OAuth

**Files:** migration `0002_rate_limits.sql`, `internal/ratelimit/limiter.go`,
`internal/httpapi/ratelimit.go`, `internal/sms/sms.go`, `internal/httpapi/auth.go`,
`internal/config/config.go`

- `Limiter.Allow(ctx, bucket, limit, window) (allowed bool, retryAfter time.Duration)`
  over `rate_limits(bucket PK, count, window_start)` with fixed windows.
- Applied per §Rate limits: otp start 3/phone/15m + 5/IP/15m, otp verify 5/phone/15m
  then 15m lock, queue 5/min, invites 10/h, messages 20/min/thread, reports 10/day
  (except csam/underage), photos 12/h, session poll 2/s.
- 429 body adds `Retry-After`.
- Turnstile: after 2 IP failures in the window, `/auth/otp/start` requires a valid
  `turnstileToken` (verified against Cloudflare only when `TURNSTILE_SECRET` is set;
  otherwise logged and allowed).
- `sms.Provider` interface + `LogProvider` (default) and `TwilioProvider`
  (`TWILIO_ACCOUNT_SID`/`TWILIO_AUTH_TOKEN`/`TWILIO_FROM`). Kill switch flag
  `auth.otp=false`. `devCode` still returned only when `APP_DEBUG`.
- OAuth: keep the debug demo; add `internal/oauth` with a Google ID-token
  verifier (`GOOGLE_CLIENT_ID`) and an Apple verifier that returns
  "not configured" without `APPLE_CLIENT_ID`/`APPLE_TEAM_ID`/`APPLE_KEY_ID`+key.

## Phase 3 — Idempotency keys

**Files:** migration `0003_idempotency.sql`, `internal/httpapi/idempotency.go`

- `Idempotency-Key` on `POST /queue`, `POST /invites`, `POST /pairs/:id/connect`,
  `POST /threads/:id/messages`.
- First call stores `{status, body}`; a replay returns it verbatim. Same key with
  a different body hash → 409 `idempotency_conflict`.

## Phase 4 — Missing endpoints, block/expiry rules

**Files:** `internal/httpapi/matches.go`, `internal/httpapi/me.go`,
`internal/httpapi/safety.go`, `internal/game/engine.go` (ticker), `internal/store/*`

- `GET /matches` — `state=mutual` only, with `pairId` + `threadId`.
- `GET /me/blocks` — the caller's directed blocks.
- `GET /me/export` — one JSON document of the caller's data.
- `POST /blocks` also cancels pending invites both ways (§Block).
- Ticker expires matches past `expires_at`: `matches.state=expired`,
  `pair.state=expired` with **null** cooldown, thread `locked`.

## Phase 5 — Game protocol completion

**Files:** `internal/game/engine.go`, `internal/game/view.go`, `internal/game/scorer.go`

- Guess My Answer two-phase with `rounds.extra.phase` (`answer` → `guess`),
  8s then 12s; answer-phase timeout skips the round (no signals), guess timeout
  is an incorrect guess; `RoundView.phase` exposed and only the owning role may
  submit per phase.
- 20 Questions timer 12s → 20s.
- Signals: `gma_answer` / `gma_guess` with `{correct}`.

## Phase 6 — `scorer_v0` + behavior stats

**Files:** migration `0004_scorer.sql` (traits.axis, behavior-stat columns,
`pair_current_scores`), `internal/game/scorer.go`, `internal/game/stats.go`

- Sets: `P` = personality traits; `L` = lifestyle traits ∪ `lifestyle.*` picked
  tags; `I` = interest traits ∪ favorite games ∪ `interest.*` picked tags.
- `behavior_sim` per §Behavior vector (Laplace `(sum+1)/(n+2)` per dimension with
  `n>0`, cosine over dims both observed, `0.5` when none).
- Location 1.0/0.5/0.0; `/compatibility_snapshots` + `pair_current_scores` written.
- Percent hidden unless both sides have ≥8 tagged non-practice, non-timeout answers.
- Reason table implemented with its rank keys and the same-energy bonus.
- `user_behavior_stats` updated from signals at session completion.
- Existing tests updated for the new expectations; scorer gets its own unit tests.

## Phase 7 — Geolocation → metro

**Files:** `internal/geo/geohash.go`, `internal/httpapi/me.go`, store

- `POST /me/location {lat,lng}`: nearest metro by centroid distance (bbox when
  present), sets `metro_id` + `city_label` + `approx_geohash` (precision 5) +
  `approx_geohash_source=geo`; raw coordinates are dropped and never logged.
- Outside every metro → no change, `{ok:true}`.

## Phase 8 — Web Push

**Files:** migration `0005_push.sql`, `internal/push/push.go`,
`internal/httpapi/push.go`, `apps/web/src/push.ts`, `apps/web/vite.config.ts`

- `push_subscriptions(user_id, endpoint UNIQUE, p256dh, auth, ua, created_at)`.
- `GET /push/vapid-public-key`, `POST /me/push`, `DELETE /me/push`.
- Sends on mutual match, invite creation, and new message; no-op without
  `VAPID_PUBLIC_KEY`/`VAPID_PRIVATE_KEY`.
- Frontend: opt-in button on Me, `PushManager.subscribe`, and a push handler
  registered via Workbox `importScripts` so `generateSW` keeps working.

## Review Focus

| Risk | Where the test lives |
| --- | --- |
| A rate limit that resets on restart, or drops a `csam` report | Phase 2 (`TestReportCsamSurvivesRateLimit`) |
| Idempotent replay double-acting (two sessions, two invites) | Phase 3 (`TestQueueIdempotentReplay`) |
| Block leaving a pending invite deliverable | Phase 4 (`TestBlockCancelsPendingInvites`) |
| Percent shown on a short session | Phase 6 (`TestPercentHiddenBelowEightAnswers`) |
| GMA phase confusion (guesser submitting in the answer phase) | Phase 5 (`TestGMAWrongRoleRejected`) |
| Export leaking phone/DOB/geohash | Phase 4 (`TestExportOmitsSecrets`) |
| Push fan-out blocking the request path | Phase 8 (`TestPushSendIsNonBlocking`) |

## Execution

Phases run in order, each ending green (`go test ./... -race`, `go vet`, `gofmt`,
`pnpm --filter web build`) with its own commit. Third-party integrations are
verified with fakes; the operator supplies credentials at deploy time.
