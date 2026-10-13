# Docker Deployment — Design

**Date:** 2026-10-13
**Status:** Draft for review

## Goal

Run GameMatch under Docker Compose, tested on a local machine first and then
deployed to a VPS with no change to the compose file beyond `.env`.

Today the app is deployed with a hand-assembled combination of a systemd unit, a
hand-written Caddyfile and scripts. `docs/runbooks/deploy.md` assumes a specific
directory layout (`/opt/gamematch`, `/var/lib/gamematch`, `/backup/gamematch`)
and `scripts/backup.sh` and `deploy/gamematch-backup.service` are written
against it. That layout is the only thing the deploy depends on, and the
audit found two defects in the shipped `deploy/Caddyfile` that make it a poor
starting point (§ Non-goals).

## Decisions

Settled with the developer before writing this spec:

| Decision | Choice | Why |
| --- | --- | --- |
| Data location | Bind mount `./data` | The container is disposable; the directory is the state. Moving to a new host is `rsync -a ./data`. |
| TLS | Caddy inside the compose stack | Automatic ACME; no nginx or certbot; matches the existing proxy model. |
| Secrets | Gitignored `.env`, read by compose | Never baked into an image or the repo. Requires the `.gitignore` fix from #97. |
| Local posture | `APP_ENV=local` locally, `APP_ENV=production` on the VPS | Production mode cannot sign anyone in without real Twilio credentials (§ Why production mode cannot run locally). |
| Backups | Scheduled container running `scripts/backup.sh` | The script's correctness work is already done and tested in PR #103. |
| Images | Two: Go API, and Caddy serving the built PWA | pnpm and Node never enter a runtime image. |

## Why production mode cannot run locally

Worth stating explicitly, because it contradicts an intuitive reading of
"same stack in both places".

`Config.Validate()` (added in PR #103) refuses to serve production traffic
unless `SMS_PROVIDER=twilio` with complete credentials. When it is set, the OTP
is delivered through the real Twilio API. Against dummy credentials the send
fails, and the code is never shown to the user: `devCode` is only returned when
`APP_DEBUG` is on, which production forbids.

The provider that logs the code is reachable only outside production. That is
deliberate — PR #103 removed OTP logging from the journal precisely because a
half-configured Twilio account silently wrote one-time codes to disk.

So the local stack runs `APP_ENV=local`: the log provider applies, OTP is
`123456`, and `seed` provides demo users. On the VPS, `.env` switches to
`APP_ENV=production` with real credentials. The compose file is identical.

The risk this leaves is drift: a change that only works in `APP_ENV=local` will
not be caught locally. Mitigated by § Verification, which boots the stack in
production mode with placeholder credentials purely to confirm `Validate()`
passes and the service starts — not to sign in.

## Architecture

Three services in one `docker-compose.yml`.

```
                    host ./data  ──┐
                                    │  (bind mount, read-write)
   ┌────────────────────────────────┴─────────┐
   │  api        0.0.0.0:8000   (no ports:)  │
   │  Go binary, DATA_DIR=/data              │
   └───────────────────┬──────────────────────┘
                       │ internal network, http://api:8000
   ┌───────────────────┴──────────────────────┐
   │  web    caddy    publishes 80, 443        │
   │  serves apps/web/dist, proxies /v1        │
   └──────────────────────────────────────────┘

   ┌──────────────────────────────────────────┐
   │  backup   alpine + sqlite3               │
   │  runs scripts/backup.sh on a schedule    │
   │  reads ./data, writes ./backups          │
   └──────────────────────────────────────────┘
```

### Why `ADDR` changes inside the container

`deploy/gamematch.env.example` sets `ADDR=127.0.0.1:8000`. That is correct on a
host — the service should not be reachable except through Caddy — but inside a
container `127.0.0.1` is the container's own loopback, so Caddy could never
reach the API.

The container therefore sets `ADDR=0.0.0.0:8000`. The exposure this creates is
contained by **not publishing the port**: `api` declares no `ports:` mapping, so
it is reachable only from the compose network. Publishing `8000` would put the
API directly on the host's internet-facing interface, bypassing TLS and every
header in the Caddyfile.

This is the single most important line in the compose file. Verification asserts
it (§ Verification).

### Services

**`api`** — `apps/server/Dockerfile`, multi-stage:

- Build stage: `golang:1.27.2-alpine`, `CGO_ENABLED=0` (the SQLite driver is
  pure Go), producing a static binary.
- Runtime stage: `alpine` with `ca-certificates` and `tzdata` only, plus a
  non-root user. The binary is copied in; no toolchain, no source.

The image contains no `dist/`. The web app is served by Caddy, not the API.

**`web`** — `caddy:2.10.2-alpine` (pinned to a minor version), running `deploy/Caddyfile`. Built separately
by copying `apps/web/dist` into the image, so the web artifact is rebuilt only
when it changes and the API image is not invalidated by it.

**`backup`** — `alpine` with `sqlite3` installed. `scripts/backup.sh` exits
non-zero if `sqlite3` is missing rather than silently skipping, so the
requirement is explicit. Mounted read-only on `./data`.

Implemented as a long-running container that sleeps 6 hours between runs
rather than as a compose-native scheduler: compose has no cron, and a `sleep`
loop is inspectable with `docker compose logs backup`. It runs once
immediately on start, then every 6 hours, so a fresh `up` takes a first backup
without waiting. This matches the systemd timer's hourly cadence closely enough
while reducing how often the 14-day prune has to run.

## Data

`./data` on the host, bind-mounted at `/data`:

```
./data/
  gamematch.db
  gamematch.db-wal
  gamematch.db-shm
  photos/
```

`DATA_DIR=/data` inside the container. `store.Config.DBPath()` and
`PhotosDir()` both derive from `DATA_DIR`, so no code change is needed.

**WAL sidecars matter here.** The service runs SQLite in WAL mode. The
`-wal` and `-shm` files are part of the database's state; a backup or a copy
that omits them loses uncheckpointed commits, including the schema. This is why
`scripts/backup.sh` uses `sqlite3 .backup` and never `cp` — verified in PR #103,
where copying only the `.db` against a live WAL produced a database with no
schema at all.

**UID alignment.** The `api` and `backup` containers run as a fixed non-root
UID (10001). `./data`
is created on the host by that UID so the container can write it without
`chown` on every deploy. Documented in the runbook, since it is the most likely
first-run failure.

**No `down -v` on the data directory.** `docker compose down -v` removes named
volumes; a bind mount is host state and survives regardless, which is the
property we want.

## Backups

`backup` runs `scripts/backup.sh` every 6 hours, writing to `./backups`:

```
./backups/gamematch-<stamp>.db     sqlite3 .backup, verified
./backups/photos-<stamp>.tar.gz    tar of ./data/photos
```

14-day retention, pruned by the script. At 6-hourly runs that is 56 snapshots
retained.

This reuses the script from PR #103 unchanged. It was written for systemd but
takes `DATA_DIR` from the environment and its output directory as `$1`, so it
needs no modification.

**Off-box is still manual.** `docs/runbooks/restore.md` documents the restore
procedure. Copying `./backups` off the host remains the developer's
responsibility; the audit flagged this and it is out of scope here.

## Changes to `deploy/Caddyfile`

The shipped Caddyfile has three defects, all verified:

1. **`root * {$GAMEMATCH_ROOT:apps/web/dist}` is relative.** Caddy's packaged
   unit has no `WorkingDirectory`, so it resolves from `/` and every static
   asset 404s. The API health check still returns 200, so `deploy.md`'s own
   verify step passes while the site is broken.
2. **The cache rule misses deep routes.** `@shell path / /index.html /sw.js
   /registerSW.js /manifest.webmanifest` is an exact-match allowlist, but
   `try_files {path} /index.html` serves the shell for *every* unmatched path.
   `/boot` — the PWA's own `start_url` — was uncached, so returning users could
   hold a stale `index.html` pointing at old content-hashed assets.
3. **The access log has no rotation.** Caddy's `output file` does not roll
   without `roll_size`/`roll_keep`, and no logrotate config ships.

Plus two corrections to documentation that is currently wrong: an unset
`GAMEMATCH_SITE` binds `:443` with Caddy's internal CA, not plain HTTP on
`:80`; and `-X-Powered-By` is dead config that Caddy never emits.

The container design resolves #1 by construction — the root is a fixed path
inside the image, with no environment-dependent working directory. #2 and #3
are fixed in the Caddyfile. Security headers (CSP, HSTS, X-Frame-Options,
Permissions-Policy), which the audit found missing, are added at the same time.

**A caveat, recorded honestly:** while verifying the Caddyfile locally, the
`caddy` binary available on this machine turned out to be a broken build that
panicked on any config, including a two-line one. That was a local tooling
problem, not a repo problem, and a freshly built binary works. But it means the
Caddy changes in this work will be verified by running the compose stack, not
by the local `caddy` binary. If verification cannot be completed in this
environment, that must be stated rather than assumed.

## Files

| Path | Action |
| --- | --- |
| `apps/server/Dockerfile` | New. Multi-stage, `CGO_ENABLED=0`, non-root. |
| `apps/web/Dockerfile` | New. Builds nothing; copies `dist/` into `caddy`. |
| `docker-compose.yml` | New. Three services. |
| `.dockerignore` | New. Keeps `node_modules`, `data`, `backups`, `.git` out of build context. |
| `.env.example` | New. The `.env` compose reads; documents both local and VPS settings. |
| `deploy/Caddyfile` | Modified. Absolute root, correct cache rule, log rotation, security headers. |
| `.gitignore` | Modified. Ignore `.env`, `data/`, `backups/`. Part of #97. |
| `docs/runbooks/deploy-docker.md` | New. Local test and VPS deploy. |
| `docs/runbooks/deploy.md` | Modified. Points to the Docker runbook as the supported path. |
| `docs/runbooks/restore.md` | Modified. Container-aware restore steps. |

No Go or TypeScript source changes. No dependency changes. No schema changes.

## Local test flow

```bash
cp .env.example .env          # APP_ENV=local
docker compose up --build
docker compose run --rm api seed     # first run only; creates demo users
open http://localhost
```

Sign in with `+15551111111` / `123456`.

## VPS deploy flow

```bash
git clone <repo> && cd game-match-pwa
cp .env.example .env
$EDITOR .env                  # APP_ENV=production, APP_DEBUG=false, no DEV_OTP,
                              # SMS_PROVIDER=twilio + three TWILIO_* + TURNSTILE_SECRET
mkdir -p data backups && chown -R 10001:10001 data
docker compose up --build -d
```

Point the domain's DNS at the host before first start so Caddy can obtain a
certificate. `docker compose logs -f web` shows the ACME result.

## Verification

Run as part of this work, not deferred:

1. Both images build; the API image contains no `dist/`, no Go toolchain, and
   runs as a non-root user.
2. `docker compose config` resolves; `docker compose up` reaches a healthy
   state.
3. `GET /` through Caddy returns the app shell, 200.
4. `GET /v1/healthz` through Caddy returns 200 — proving the proxy reaches the
   API, not just that the API is up.
5. `GET /boot` returns 200 and carries `Cache-Control: no-cache`.
6. `GET /assets/<hash>.js` carries `Cache-Control: immutable`.
7. Security headers present on a static response: CSP, HSTS, X-Frame-Options,
   Permissions-Policy, nosniff.
8. **The API port is not published.** `docker compose ps` shows no host mapping
   for 8000, and a request to the host's port 8000 is refused.
9. `docker compose restart api` preserves data: a user created before the
   restart still exists after.
10. Backup produces a snapshot, and restoring it yields a database with the
    schema and the expected row count.
11. Booting with `APP_ENV=production` and placeholder credentials starts the
    service (proving `Validate()` passes) while the API port is still closed.
12. `docker compose run --rm api` with a deliberately invalid production config
    exits non-zero with a message naming the missing variable — proving the
    fail-fast from PR #103 still works through the container.

Checks 8 and 12 are the two that would catch a regression in the property this
design is most concerned with.

## Non-goals

- **Off-box backup sync.** Out of scope; documented as a manual step.
- **Horizontal scaling.** SQLite is a single writer. One API container.
- **Migrating off systemd.** The systemd path stays documented for anyone
  already running it; Docker becomes the supported path.
- **Health-based autoscaling, blue/green deploys.** Not warranted at this size.
- **A container registry.** The VPS builds from source.
- **Secret rotation runbook.** Still missing from the repo, still missing here.

## Risks

| Risk | Mitigation |
| --- | --- |
| SQLite WAL on a filesystem that does not support its locking (some VPS overlay/network filesystems) | Documented in the runbook as a filesystem requirement. Verification step 9 exercises restart-with-data, which fails loudly if locking is broken. |
| `.env` drift between local and VPS | One file, two documented configurations. Verification step 11 boots production mode locally. |
| `./data` owned by the wrong UID on the host | Runbook states the `chown` explicitly as a first-run step. |
| `APP_ENV=local` hides production-only failures | Verification step 12 exercises the production refusal path through the container. |
| Caddyfile changes unverifiable locally (broken local `caddy` binary) | Verify by running the stack, which uses the `caddy` image, not the host binary. If that is not possible here, say so rather than claiming verification. |
