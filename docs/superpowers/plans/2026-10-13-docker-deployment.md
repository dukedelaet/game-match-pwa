# Docker Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run GameMatch under Docker Compose on a local machine and on a VPS, changing only `.env` between the two, with data in a bind-mounted `./data` that survives a container rebuild.

**Architecture:** Three services in one compose file. `api` is the Go binary on `0.0.0.0:8000` with **no published port**, reachable only on the internal network. `web` is Caddy, publishing 80/443, serving `apps/web/dist` and proxying `/v1` to `api`. `backup` is an Alpine container with `sqlite3` that runs the existing `scripts/backup.sh` every 6 hours. Two multi-stage Dockerfiles, `CGO_ENABLED=0`, so pnpm and Node never enter a runtime image.

**Tech Stack:** Docker Compose, `golang:1.27.2-alpine`, `caddy:2.10.2-alpine`, `alpine` (sqlite3), SQLite in WAL mode via `modernc.org/sqlite`.

**Spec:** `docs/superpowers/specs/2026-10-13-docker-deployment-design.md` — the plan argues from the spec, so the spec travels with it; executors read both.

## Global Constraints

- `CGO_ENABLED=0` everywhere. The SQLite driver is pure Go; the binary must build and run without a C toolchain.
- The `api` container must declare **no `ports:` mapping**. It binds `0.0.0.0:8000` because container loopback is not reachable from a sibling container. Publishing it would expose the API directly on the host, bypassing TLS and every header in the Caddyfile.
- `DATA_DIR=/data` in both `api` and `backup`. Every SQLite path derives from it (`store.Config.DBPath()`, `PhotosDir()`), so no Go code changes.
- The container runs as UID **10001**, non-root. `./data` is created on the host by that UID.
- No Go or TypeScript source changes. No dependency additions. No schema changes.
- `APP_ENV=local` locally (OTP `123456`, log SMS provider, `seed` demo users). `APP_ENV=production` on the VPS with real credentials. The compose file is identical between them.
- `scripts/backup.sh` is reused **unchanged**. It already takes `DATA_DIR` from the environment and its output directory as `$1`.

## Review Focus

Five input classes the spec implies that no task's tests exercise. Each gets a test in the task that owns the relevant code.

1. **The API port is published by accident.** A `ports:` line added to `api`, or a `network_mode: host`, silently exposes the API on the host's public interface with no TLS. A reasonable person would expect the API to be unreachable from outside the compose network. → Task 5, step 2.
2. **`./data` owned by the wrong UID.** The container cannot write the database and the service dies on boot with a permission error that names neither the host path nor the UID. → Task 5, step 3.
3. **SQLite on a filesystem that does not support WAL locking.** Some VPS overlay and network filesystems break WAL. The service starts and then fails on first write, or corrupts. → Task 5, step 8.
4. **A production config missing one variable.** `Config.Validate()` refuses to boot and names the variable — but only if the refusal survives the container boundary. If the entrypoint swallows the exit code, a VPS deploys a silently non-functional service. → Task 6, step 4.
5. **Restoring a backup onto a stale `-wal`.** The restored database is replayed against the live sidecar and silently corrupts. → Task 7, step 5.

---

### Task 1: Fix `deploy/Caddyfile`

The shipped Caddyfile has four defects, all verified in the audit, and it **panics Caddy on startup** when `/var/log/caddy` does not exist. Doing this first means the container work in later tasks inherits a working proxy, and #95/#96 stop being separate half-finished work.

**Files:**
- Create: `deploy/caddy-verify.sh` — verification harness
- Modify: `deploy/Caddyfile`

**Interfaces:**
- Consumes: `apps/web/dist/` (built output), `http://api:8000` (the `api` service, from Task 3).
- Produces: a `deploy/Caddyfile` that works with an absolute `GAMEMATCH_ROOT`, an existing log directory, and no environment-dependent working directory. Task 2's `web` image copies this file verbatim.

**Note on verification:** do **not** use the host `caddy` binary. It panics on
every config, including a two-line one and a pure-JSON config with no modules:
Caddy 2.10.2 is incompatible with Go 1.27 (`expected ModuleMap because
inline_key is empty`). The `caddy:2.10.2-alpine` image bundles its own
toolchain, is unaffected, and was confirmed to start and serve. Every
verification below runs the image.

On this machine, Docker requires the `docker` group and the session must have
been started after joining it. Wrap Docker commands with
`newgrp docker <<< "$*"` if the socket returns permission denied.

The harness deliberately sets an absolute `GAMEMATCH_ROOT` and creates the log
directory, so that Step 2 isolates the cache-rule and header defects from the
relative-root defect. The relative root is verified separately in Task 5
step 3, where it is the real problem.

- [ ] **Step 1: Write the verification harness**

`deploy/caddy-verify.sh` starts Caddy against a test root and asserts the four behaviours. It must exit non-zero on any failure.

```bash
#!/usr/bin/env bash
# Verifies deploy/Caddyfile against a built apps/web/dist, without touching
# the host's caddy binary (the one at /home/duke/.local/share/mise is a broken
# build that panics on any config) and without needing a real domain.
#
# Usage: bash deploy/caddy-verify.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${CADDY_VERIFY_PORT:-9444}"
LOGDIR="$ROOT/.caddytest"
WORKDIR="$ROOT/apps/web/dist"

fail=0
check() { # check <label> <expected> <actual>
  if [ "$2" = "$3" ]; then printf '  ok   %s\n' "$1"
  else printf '  FAIL %s: expected %s, got %s\n' "$1" "$2" "$3"; fail=1; fi
}
header() { # header <path> <header>  -> value or ""
  curl -sk -o /dev/null -D - "https://127.0.0.1:$PORT$1" 2>/dev/null \
    | tr -d '\r' | awk -v h="$2:" 'tolower($1)==tolower(h){$1="";sub(/^ /,"");print;exit}'
}
status() { curl -sk -o /dev/null -w '%{http_code}' "https://127.0.0.1:$PORT$1"; }

if [ ! -f "$WORKDIR/index.html" ]; then
  echo "caddy-verify: $WORKDIR/index.html missing; run 'pnpm --filter web build' first" >&2
  exit 1
fi

# A log directory must exist: Caddy panics on startup if the directory for
# `output file` does not, rather than reporting a usable error.
rm -rf "$LOGDIR"; mkdir -p "$LOGDIR"
mkdir -p "$ROOT/.caddytest"

GAMEMATCH_ROOT="$WORKDIR" GAMEMATCH_SITE="localhost:$PORT" \
GAMEMATCH_LOG_DIR="$LOGDIR" \
docker run -d --name gm-caddy-verify -p "$PORT:$PORT" \
  -v "$ROOT/deploy/Caddyfile:/etc/caddy/Caddyfile:ro" \
  -v "$WORKDIR:/srv/dist:ro" \
  -v "$LOGDIR:/var/log/caddy" \
  caddy:2.10.2-alpine caddy run --config /etc/caddy/Caddyfile >/dev/null

# Wait for the listener rather than sleeping a fixed amount.
for _ in $(seq 1 40); do
  if [ "$(status /)" != "000" ]; then break; fi
  sleep 0.25
done

echo "caddy-verify:"
check "GET / serves the shell"            200 "$(status /)"
check "GET /boot (PWA start_url) serves"   200 "$(status /boot)"
check "GET /deep/spa/route falls back"     200 "$(status /deep/spa/route)"
check "/boot is not cached"                "no-cache" "$(header /boot Cache-Control)"

asset="$(ls "$WORKDIR/assets" | grep -m1 '\.js$')"
check "asset is immutable"                 "public, max-age=31536000, immutable" \
      "$(header "/assets/$asset" Cache-Control)"
check "nosniff present"                    "nosniff" "$(header / X-Content-Type-Options)"
check "CSP present"                        "1" "$(test -n "$(header / Content-Security-Policy)" && echo 1 || echo 0)"
check "HSTS present"                       "1" "$(test -n "$(header / Strict-Transport-Security)" && echo 1 || echo 0)"
check "X-Frame-Options present"            "1" "$(test -n "$(header / X-Frame-Options)" && echo 1 || echo 0)"

docker rm -f gm-caddy-verify >/dev/null 2>&1 || true
rm -rf "$ROOT/.caddytest"
exit "$fail"
```

- [ ] **Step 2: Run it to verify it fails**

Run: `pnpm --filter web build && bash deploy/caddy-verify.sh`

Expected: FAIL. Specifically `/boot is not cached` (the `@shell` matcher is an exact-match allowlist that misses it) and `CSP present` / `HSTS present` / `X-Frame-Options present` (no such headers are set). `/`, `/boot` and the SPA fallback should already be 200 because the harness supplies an absolute `GAMEMATCH_ROOT` and an existing log directory — that is the harness isolating one defect at a time, and it is why Steps 3–4 can be verified separately.

- [ ] **Step 3: Rewrite `deploy/Caddyfile`**

Three changes, all from the spec:

- **Absolute root.** `root * {$GAMEMATCH_ROOT:/srv/dist}`. The default resolves inside the image where it always exists, with no environment-dependent working directory — which is what makes the container design safe. `$GAMEMATCH_LOG_DIR` replaces the hardcoded `/var/log/caddy`.
- **Cache rule stated as the complement of the asset rule.** `try_files {path} /index.html` serves the shell for *every* unmatched path, so the old exact-match `@shell` allowlist of four paths left the rest with no `Cache-Control` at all. Both rules must become `@assets path /assets/*` and `@shell not path /assets/*` — disjoint matchers, so the response gets exactly one `Cache-Control` whatever order Caddy emits the header ops in. **Do not set `no-cache` unconditionally and override it for assets**: Caddy emits matched header ops first and `Set` is last-write-wins, so every content-hashed asset would ship `no-cache` and lose its immutable caching. This was verified by adapting both forms and reading the resulting JSON.
- **Security headers and log rotation.** CSP, HSTS, X-Frame-Options, Permissions-Policy; `roll_size 100MiB`, `roll_keep 10`, `roll_keep_for 720h`. Drop the `-X-Powered-By` line, which Caddy never emits. Correct the header comment: an unset `GAMEMATCH_SITE` binds `:443` with Caddy's internal CA, not plain HTTP on `:80`.

- [ ] **Step 4: Run it to verify it passes**

Run: `bash deploy/caddy-verify.sh`

Expected: every line `ok`, exit 0.

- [ ] **Step 5: Confirm Caddy itself is well-formed**

Run: `docker run --rm -v "$PWD/deploy/Caddyfile:/etc/caddy/Caddyfile:ro" caddy:2.10.2-alpine caddy validate --config /etc/caddy/Caddyfile`

Expected: `Valid configuration`. This is what the host binary could not be trusted to tell us.

- [ ] **Step 6: Commit**

```bash
git add deploy/Caddyfile deploy/caddy-verify.sh
git commit -m "fix(deploy): absolute root, correct cache rule, security headers

The shipped Caddyfile resolved \$GAMEMATCH_ROOT relative to Caddy's working
directory, which for the packaged unit is /, so every static asset 404'd
while the API health check still passed. The @shell cache matcher was an
exact-match allowlist of four paths, so the PWA's own start_url /boot was
served uncached and returning users could hold a stale index.html. The
access log had no rotation, and Caddy panics on startup rather than
reporting a usable error when the log directory does not exist.

Adds the security headers the audit found missing, and a verification
harness that runs the published caddy image rather than the host binary."
```

---

### Task 2: Container images

**Files:**
- Create: `apps/server/Dockerfile`
- Create: `apps/web/Dockerfile`
- Create: `.dockerignore`
- Modify: `.gitignore`

**Interfaces:**
- Consumes: `scripts/backup.sh` (Task 3 mounts it), `deploy/Caddyfile` (Task 1, copied into the web image), `apps/web/dist` (built output).
- Produces: images `gamematch-api:latest` and `gamematch-web:latest`. Task 3's compose file references both by name.

- [ ] **Step 1: Write the failing verification**

Run these before the files exist.

```bash
docker build -t gamematch-api:latest -f apps/server/Dockerfile . && \
  docker run --rm --entrypoint id gamematch-api:latest -u
```

Expected: FAIL — no `Dockerfile` in `apps/server`.

- [ ] **Step 2: Create `apps/server/Dockerfile`**

Multi-stage. Build stage `golang:1.27.2-alpine` matching `mise.toml`; runtime base pinned to `alpine:3.24` with `ca-certificates` and `tzdata` only. Non-root UID 10001 with a matching `/data`.

Set `ENTRYPOINT ["/usr/local/bin/gamematch"]`, not `CMD`: the binary is the program, so `docker run <image> seed` must run `gamematch seed`. Verify the UID with `docker run --rm --entrypoint id gamematch-api:latest -u` — using `CMD` to satisfy that assertion would invert the image's default semantics.

- [ ] **Step 3: Create `apps/web/Dockerfile`**

`caddy:2.10.2-alpine`, copying `apps/web/dist` to `/srv/dist` and `deploy/Caddyfile` to `/etc/caddy/Caddyfile`. This image builds nothing — it serves prebuilt output, so a web change does not invalidate the API image.

- [ ] **Step 4: Create `.dockerignore`**

Exclude `node_modules`, `.git`, `apps/web/dist`, `apps/server/data`, `data`, `backups`, `.env`, `*.log`, and `docs`. Without this, a stray `node_modules` can reach the build context and the API image becomes enormous.

- [ ] **Step 5: Extend `.gitignore`**

Add `.env` variants (`#97` notes `.env.*` is not ignored), `/data/`, `/backups/`, `.caddytest/`. Keep `!.env.example` un-ignored.

- [ ] **Step 6: Build both images and verify the API image is clean**

```bash
docker build -t gamematch-api:latest apps/server
docker build -t gamematch-web:latest apps/web
docker run --rm --entrypoint id gamematch-api:latest -u   # 10001
docker run --rm --entrypoint ls gamematch-api:latest -d /srv/dist   # must fail: no dist in the API image
docker run --rm --entrypoint sh gamematch-api:latest -c 'which go'  # must fail: no toolchain
docker run --rm gamematch-web:latest ls /srv/dist/index.html
```

Expected: UID `10001`; both "must fail" checks fail; the web image serves the built shell.

- [ ] **Step 7: Commit**

```bash
git add apps/server/Dockerfile apps/web/Dockerfile .dockerignore .gitignore
git commit -m "build: multi-stage container images for the API and the PWA

CGO_ENABLED=0 throughout, so the pure-Go SQLite driver produces a static
binary and no C toolchain is needed at build or run time. The API image
carries no dist/ and no toolchain; the web image builds nothing and serves
prebuilt output, so a frontend change does not invalidate the API image."
```

---

### Task 3: Compose file and `.env`

**Files:**
- Create: `docker-compose.yml`
- Create: `.env.example`
- Create: `.env` (gitignored, local only)

**Interfaces:**
- Consumes: images from Task 2, `deploy/Caddyfile` from Task 1, `scripts/backup.sh`.
- Produces: `docker compose up` reaching a healthy state; the service names `api`, `web`, `backup`.

- [ ] **Step 1: Create `docker-compose.yml`**

Three services. `api` declares **no `ports:`**. `web` publishes `${HTTP_PORT:-80}:80` and `${HTTPS_PORT:-443}:443`. `backup` sleeps between runs.

The API image sets `ENTRYPOINT ["/usr/local/bin/gamematch"]` (Task 2), so `docker compose run --rm api seed` works with no `entrypoint:` key on the service. The API also needs `stop_grace_period: 30s` — the server drains for 25s and Docker's default is 10s.

`deploy/Caddyfile` still says `reverse_proxy 127.0.0.1:8000`, which cannot reach a compose sibling container. Change that line to `{$GAMEMATCH_API:127.0.0.1:8000}` and set `GAMEMATCH_API=api:8000` in the web service's environment, so the systemd path keeps working unchanged when no env is set.

Beware: the image's `ENV ADDR=0.0.0.0:8000` is overridden by compose `environment:` but not by a stray `ADDR=` line in `env_file:`. If one appears the API binds loopback and every proxied call 502s with no other symptom.

- [ ] **Step 2: Create `.env.example`**

Both configurations in one file, with the production section uncommented-by-default and a clearly marked local section:

```
# --- local (APP_ENV=local) ---
APP_ENV=local
APP_DEBUG=true
DEV_OTP=123456

# --- VPS (APP_ENV=production): uncomment and fill ---
# APP_ENV=production
# APP_DEBUG=false
# DEV_OTP=            must stay unset
# SMS_PROVIDER=twilio
# TWILIO_ACCOUNT_SID=
# TWILIO_AUTH_TOKEN=
# TWILIO_FROM=
# TURNSTILE_SECRET=
```

`DEV_OTP` is commented rather than empty in the local block because it must be *absent* in production — `Config.Validate()` rejects it there, and a blank line would read as present.

- [ ] **Step 3: Verify compose resolves**

```bash
cp .env.example .env
docker compose config >/dev/null && echo "compose config OK"
docker compose config | grep -A3 '^  api:' | grep -c 'published'
```

Expected: `compose config OK`, and `0` — the API must publish nothing.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml .env.example
git commit -m "build: compose stack with api, web and a scheduled backup

The api service declares no ports mapping. It binds 0.0.0.0 because
container loopback is unreachable from a sibling container, and the
exposure is contained by never publishing the port -- publishing it would
put the API on the host's public interface, bypassing TLS and every header
in the Caddyfile."
```

---

### Task 4: Backup container

**Files:**
- Modify: `docker-compose.yml` (the `backup` service, added here rather than in Task 3 so it can be verified alone)
- Modify: `scripts/backup.sh` **only if** its defaults need adjusting for the container — the spec says reuse unchanged, so first confirm they do not.

**Interfaces:**
- Consumes: `scripts/backup.sh`, `./data` (read-only), `./backups` (write).
- Produces: snapshots at `./backups/gamematch-<stamp>.db` and `./backups/photos-<stamp>.tar.gz`.

- [ ] **Step 1: Confirm the script works unmodified in a container**

```bash
docker run --rm -v "$PWD/scripts:/scripts:ro" -v "$PWD/data:/data:ro" \
  -v "$PWD/backups:/backup" alpine:3.20 sh -c \
  'apk add --no-cache sqlite3 >/dev/null && DATA_DIR=/data /bin/sh /scripts/backup.sh /backup'
```

Expected: `backup: wrote /backup/gamematch-<stamp>.db`. If it fails on `command -v sqlite3` or a path default, that is the signal to adjust — record the change rather than guessing.

- [ ] **Step 2: Add the `backup` service**

A long-running container that runs once immediately, then every 6 hours: `sh -c 'while true; do /scripts/backup.sh /backups; sleep 21600; done'`. Running on start means a fresh `up` takes a first backup without waiting 6 hours.

- [ ] **Step 3: Verify a snapshot is produced and is valid**

```bash
mkdir -p data backups
docker compose up -d backup
sleep 5
ls -la backups/
SNAP="$(ls -t backups/gamematch-*.db | head -1)"
sqlite3 "$SNAP" 'PRAGMA integrity_check;'      # ok
sqlite3 "$SNAP" 'SELECT count(*) FROM sqlite_master;'  # > 0, not 0
```

Expected: a `.db` and a `.tar.gz`; `ok`; a non-zero schema count. A snapshot with no schema is the failure mode `restore.md` documents, and the script is supposed to refuse it.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml
git commit -m "build: run the existing backup script on a 6-hourly schedule

Reuses scripts/backup.sh unchanged: it already takes DATA_DIR from the
environment and its output directory as \$1, and its sqlite3 .backup
approach is what makes a WAL-mode snapshot consistent. Runs once on start
so a fresh 'up' takes a first backup immediately."
```

---

### Task 5: End-to-end local run

**Files:** none new. This task verifies Tasks 1–4 together.

**Interfaces:**
- Consumes: the whole stack.
- Produces: evidence that the stack works, which the runbook then documents.

**Prerequisites (found necessary in Task 5 — the stack cannot be built without them):**

`apps/web/Dockerfile` copies `apps/web/dist/` and builds nothing, and `dist` is
gitignored. On a clean clone `docker compose build web` fails with
`"/apps/web/dist": not found`. Build the PWA on the host first:

```bash
pnpm install
pnpm --filter web build          # produces apps/web/dist
cp .env.example .env             # then set APP_ENV=local
mkdir -p data backups && sudo chown -R 10001:10001 data
```

- [ ] **Step 1: Start the stack and seed**

```bash
docker compose up -d --build
docker compose run --rm api seed
docker compose ps
```

Expected: `api`, `web`, `backup` up. `api seed` is idempotent.

- [ ] **Step 2: Assert the API port is NOT published**

```bash
docker compose ps --format '{{.Service}} {{.Ports}}'
curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:8000/v1/healthz
```

Expected: `api` shows no `0.0.0.0:8000->` mapping, and the curl returns `000` (connection refused). This is the single assertion that would catch a regression in the property this design exists to protect. If it returns `200`, stop and fix before continuing.

- [ ] **Step 3: Assert the shipped relative-root default is not in use**

The most dangerous of the three Caddyfile defects: with `GAMEMATCH_ROOT` unset and cwd `/`, every static asset 404s while `/v1/healthz` still returns 200, so a runbook check that probes only the API reports success.

```bash
docker compose exec web sh -c 'ls /srv/dist/index.html'   # must exist
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/assets/$(ls apps/web/dist/assets | grep -m1 '\.js$')
```

Expected: `200`. If it is `404`, the root is resolving relative to a working directory rather than to the image path.

- [ ] **Step 4: Assert the proxy reaches the API, not just that the API is up**

```bash
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/v1/healthz
```

Expected: `200`. A `000` here means Caddy cannot reach `api` — the failure that would happen if `ADDR` were left at `127.0.0.1:8000`.

- [ ] **Step 5: Assert the SPA is served with the right caching**

```bash
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/boot
curl -skI https://localhost/boot | grep -i cache-control
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/deep/spa/route
```

Expected: `200`, `Cache-Control: no-cache`, `200`.

- [ ] **Step 6: Assert security headers**

```bash
for h in Content-Security-Policy Strict-Transport-Security X-Frame-Options Permissions-Policy X-Content-Type-Options; do
  printf '%-32s %s\n' "$h" "$(curl -skI https://localhost/ | grep -ci "^$h:")"
done
```

Expected: `1` for each.

- [ ] **Step 7: Assert data survives a restart**

Three things were wrong in the first draft of this step and were corrected
against the real schema during execution. They are called out so the correction
is not undone:

1. `otp_codes` is keyed on `phone`, not `phone_e164` (`phone_e164` lives on
   `user_private`), so the OTP query was a parse error.
2. The final query did not error — it was worse. `user_private` has no
   `phone_e164_hash`, so SQLite bound the inner name to the outer `users` table,
   the subquery became correlated, and the predicate degenerated to
   `users.phone_e164_hash = users.phone_e164_hash`. It returned every user with a
   non-null hash and exited 0, so "printing a name" was satisfied by seeded
   demo users while the step asserted nothing about the user that had just
   signed in. Verified: the query returned 4 rows, which is exactly
   `count(*) WHERE phone_e164_hash IS NOT NULL`.
3. A freshly registered user has `name IS NULL` (`httpapi/auth.go:192-198`
   creates no `Name`; `store/users.go:80-89` defaults `status=pending`), so a
   bare `SELECT name` prints an empty line for it and looks like missing output.
   `quote()` makes the NULL visible.

The count is also a **loss** check, not the survival check — `after > before`
proves only that some user row exists. The identity check below is the one that
proves the right row survived, and it asserts rather than printing for eyeballs.

```bash
#!/usr/bin/env bash
set -euo pipefail
PHONE='+15551239999'

# otp/start enforces its rate limits SILENTLY (auth.go:120-125): over the limit
# it still answers {"ok":true} and stores nothing. With APP_DEBUG=true a code
# that was issued comes back as devCode, so an absent devCode means the limit
# was hit -- not that APP_ENV is wrong. Limits are 5/IP and 3/phone per 15m and
# are PERSISTED in rate_limits, so restarting api does not clear them.
curl -sk -X POST https://localhost/v1/auth/otp/start  -d "{\"phone\":\"$PHONE\"}" -o /dev/null
CODE="$(sqlite3 data/gamematch.db "SELECT code FROM otp_codes WHERE phone = '$PHONE';")"
test -n "$CODE" || { echo "could not read the OTP: APP_ENV=local, or the otp/start rate limit was hit"; exit 1; }
curl -sk -X POST https://localhost/v1/auth/otp/verify -d "{\"phone\":\"$PHONE\",\"code\":\"$CODE\"}" -o /dev/null

# Assert BEFORE restarting, so a sign-in that created nothing is not mistaken
# for a row lost across the restart.
ID_BEFORE="$(sqlite3 data/gamematch.db "SELECT u.id FROM users u JOIN user_private p ON p.user_id = u.id WHERE p.phone_e164 = '$PHONE';")"
CREATED="$(sqlite3 data/gamematch.db "SELECT created_at FROM users WHERE id = '$ID_BEFORE';")"
test -n "$ID_BEFORE" || { echo "FAIL: signing in created no user row"; exit 1; }

BEFORE="$(sqlite3 data/gamematch.db 'SELECT count(*) FROM users;')"
docker compose restart api
AFTER="$(sqlite3 data/gamematch.db 'SELECT count(*) FROM users;')"
printf 'before=%s after=%s\n' "$BEFORE" "$AFTER"

# -ge, not -gt: a re-run signs in an already-registered number, creates no row,
# and counts equal. With -gt that re-run fails claiming a user was lost.
test "$AFTER" -ge "$BEFORE" || { echo "FAIL: users went missing across the restart"; exit 1; }

ID_AFTER="$(sqlite3 data/gamematch.db "SELECT u.id FROM users u JOIN user_private p ON p.user_id = u.id WHERE p.phone_e164 = '$PHONE';")"
CREATED_AFTER="$(sqlite3 data/gamematch.db "SELECT created_at FROM users WHERE id = '$ID_AFTER';")"
test "$ID_AFTER" = "$ID_BEFORE"    || { echo "FAIL: a different user is there after the restart"; exit 1; }
test "$CREATED_AFTER" = "$CREATED" || { echo "FAIL: the row was replaced, not preserved"; exit 1; }

echo "same row survived: id=$ID_AFTER created_at=$CREATED_AFTER"
sqlite3 data/gamematch.db "SELECT u.id, quote(u.name), u.status, u.created_at FROM users u JOIN user_private p ON p.user_id = u.id WHERE p.phone_e164 = '$PHONE';"

# The check has to be able to say "no": same query, a phone that never signed in
# returns zero rows. The tautological version returns every user with a hash.
test -z "$(sqlite3 data/gamematch.db "SELECT u.id FROM users u JOIN user_private p ON p.user_id = u.id WHERE p.phone_e164 = '+15559990000';")" \
  || { echo "FAIL: the query matched a phone that never signed in"; exit 1; }

sqlite3 data/gamematch.db 'PRAGMA journal_mode;'
```

Expected: `before`/`after` printed, `same row survived:`, one row for `$PHONE`
with `quote(u.name)` showing `NULL`, and `wal`.

This proves the bind mount is the real state, and it exercises WAL locking — a
filesystem that cannot support it fails right here. `APP_ENV=local` must be set or
the OTP cannot be read, which is why the step asserts `$CODE` is non-empty
rather than assuming it — but an empty `$CODE` has a second cause, the silent
rate limit, which is why the message names both.

- [ ] **Step 8: Assert the data directory is host-owned, not root-owned**

```bash
ls -ld data
docker compose exec api ls -ld /data
```

Expected: both report UID 10001. A mismatch here is the Review Focus item about ownership, and it is the most likely first-run failure on the VPS.

Do **not** assert on `data/photos`. It is absent on a fresh stack: the image
builds `/data/photos`, the `./data` bind mount shadows it, and only the photo
upload handler's `MkdirAll` creates it. `ls -ld data data/photos` therefore
fails on a correct stack. This was found in Task 5 and confirmed. It is benign
while the directory is absent (there are no photos to archive), but if the
directory is later lost while the database still holds photo rows, every
subsequent backup becomes silently database-only — `scripts/backup.sh` skips
the archive without warning, exits 0, and the container stays `healthy`.

- [ ] **Step 9: Assert logs and rotation**

```bash
docker compose logs --tail 20 web
docker compose exec web ls -la /var/log/caddy
```

Expected: Caddy started cleanly with no panic, and the access log exists with `roll_size` configured.

- [ ] **Step 10: Record the evidence**

Paste the actual outputs of steps 2–8 into `docs/runbooks/deploy-docker.md` as a "verified on local Docker" section. Do not paraphrase; the next person needs to see the real numbers.

---

### Task 6: Production boot path

Local runs use `APP_ENV=local`, so nothing so far exercises `Config.Validate()`. This task closes the Review Focus gap about a missing production variable being swallowed at the container boundary.

**Files:** none new. `.env` is modified and restored.

**Interfaces:**
- Consumes: the stack from Task 5.
- Produces: evidence that the production boot path works and still fails loudly.

- [ ] **Step 1: Boot in production mode with placeholder credentials**

```bash
cp .env .env.local.bak
sed -i 's/^APP_ENV=.*/APP_ENV=production/; s/^APP_DEBUG=.*/APP_DEBUG=false/' .env
sed -i '/^DEV_OTP=/d' .env
printf 'SMS_PROVIDER=twilio\nTWILIO_ACCOUNT_SID=ACplaceholder\nTWILIO_AUTH_TOKEN=placeholder\nTWILIO_FROM=+15550000000\nTURNSTILE_SECRET=0xplaceholder\n' >> .env
docker compose up -d api
sleep 5
docker compose ps api
```

The `sed -i '/^DEV_OTP=/d'` is **required**. `.env.example`'s local block ships
`DEV_OTP=123456` uncommented, and `Config.Validate()` rejects any non-empty
`DevOTP` in production (`internal/config/config.go:126-128`) — so without that
line the container crash-loops with
`config: unsafe production configuration: DEV_OTP`. `DevOTP` is populated with
`getenv("DEV_OTP", ...)`, which only falls back to the default when the variable
is *absent or empty*, so a leftover line is fatal rather than ignored. Found in
Task 6.

Expected: `api` is **running**, not restarting. It cannot deliver a real OTP against placeholder credentials — that is expected and is why Task 5 covers signing in. This step proves `Validate()` passes and the process starts.

- [ ] **Step 2: Assert the API port is still closed in production mode**

```bash
curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:8000/v1/healthz
```

Expected: `000`. Environment must not change this.

- [ ] **Step 3: Assert APP_DEBUG is genuinely off**

```bash
curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15551230000"}'
```

Expected: `{"ok":true}` with **no `devCode` key**. If `devCode` appears, `APP_DEBUG` is on in production — the exact hole `Config.Validate()` was written to close.

- [ ] **Step 4: Assert a missing variable still fails the boot**

```bash
sed -i '/^TURNSTILE_SECRET=/d' .env
docker compose up -d --force-recreate api
sleep 5
docker compose logs api | tail -20
docker compose ps api
```

Expected: the container **exits non-zero**, and the log names `TURNSTILE_SECRET`. A container that stays up here means the entrypoint swallowed the exit code, which would let a VPS deploy a silently non-functional service.

- [ ] **Step 5: Restore local config**

```bash
mv .env.local.bak .env
docker compose up -d --force-recreate api
docker compose ps
```

Expected: `api` running, `APP_ENV=local`.

- [ ] **Step 6: Record the evidence**

Append the actual output of steps 1–4 to `docs/runbooks/deploy-docker.md` under a "Verified: production boot path" heading. This task changes no tracked code, but its result is the only evidence that `Validate()` works through a container, and it is not reproducible from the repo alone.

- [ ] **Step 7: Commit**

```bash
git add docs/runbooks/deploy-docker.md
git commit -m "docs: record the verified production boot path

Local runs use APP_ENV=local, so nothing in the normal test flow exercises
Config.Validate(). This records a real production-mode boot with
placeholder credentials, and a boot with TURNSTILE_SECRET removed that
correctly exits non-zero naming it."
```

---

### Task 7: Runbooks

**Files:**
- Create: `docs/runbooks/deploy-docker.md`
- Modify: `docs/runbooks/deploy.md`
- Modify: `docs/runbooks/restore.md`

**Interfaces:**
- Consumes: the verified output from Task 5 and Task 6.
- Produces: the documents someone follows on the VPS.

- [ ] **Step 1: Write `docs/runbooks/deploy-docker.md`**

Local test flow, then VPS deploy. The VPS section must include, in order: clone, `cp .env.example .env`, fill in the production variables, `mkdir -p data backups && chown -R 10001:10001 data`, `docker compose up --build -d`. State that DNS must point at the host **before** first start so Caddy can obtain a certificate.

Two steps belong between `chown` and `up --build -d`, both found in Task 5 and Task 6:

- **`pnpm install && pnpm --filter web build`**, because `apps/web/dist` is
  gitignored and the web image copies it without building. A clean clone fails
  the build with `"/apps/web/dist": not found`.
- **Filling in `.env` by swapping blocks, not by editing `APP_ENV`.** The order
  above is preserved: the transformation belongs in "fill in the production
  variables", which is where it is. `.env.example` documents it and it is
  published as two tested `sed` commands — the `sed` in Task 6 step 1 is the
  thing that produced the `DEV_OTP` crash loop.

Include a troubleshooting table keyed to the failures this stack actually produces: API unreachable through Caddy (`ADDR` left at loopback), permission denied on `./data` (UID), Caddy certificate failure (DNS not yet pointed), and a database that will not open (filesystem without WAL support).

Beyond the four the plan names, the rows this stack actually produced during
Tasks 4–7: `server misbehaving` from Docker's embedded DNS while `api`
crash-loops (which is *not* `connection refused`, and editing `GAMEMATCH_API`
fixes nothing); `docker compose ps` reporting a crash loop as
`Up 1 second (health: starting)`; `.State.ExitCode` reading `0` during a crash
loop; `otp/start` returning `{"ok":true}` and storing nothing once a
*persisted* rate limit is hit; a missing `data/photos` making every backup
silently database-only while the container stays `healthy`; `caddy reload`
failing because the Caddyfile sets `admin off`; and `env: can't execute 'bash'`
from a manual backup run with `--entrypoint sh`.

The `.env` section must tell **one** story about how values are set. Appending a
corrected value works (Compose's `env_file` takes the last occurrence) but
leaves the empty original above it, which is the same ambiguity as the `DEV_OTP`
bug and makes any "is this key still empty?" check permanently red. Publish
in-place editing, and make the check compare *keys* — `grep -E '^[A-Z]' .env |
cut -d= -f1 | sort | uniq -d` — not whole lines, which does not detect two
different values for one key.

Do **not** tell a reader to run a production host in `APP_ENV=local` to make a
drill easier. `otp_codes.code` is plaintext (`internal/store/auth.go:59-63`), so
the drill works in production mode with no such step; the instruction would only
make every code the constant `123456` and return it in the response.

- [ ] **Step 2: Repoint `deploy.md`**

Mark the systemd path as legacy and link the Docker runbook as supported. Keep the systemd instructions — someone may already be running them — but do not leave them reading as the primary path.

- [ ] **Step 3: Add container-aware steps to `restore.md`**

The `rm -f "$DATA_DIR"/gamematch.db-{wal,shm}` step becomes `docker compose stop api` first, so no live process rewrites the sidecars mid-restore. Keep the existing warning about not copying a live `.db`.

Two corrections found while executing this step, both now in `restore.md`:

1. **The recovery point objective is up to 6 hours, not 1.** This file says 6
   hours at lines 7 and 340 and the design spec agrees, but `restore.md` still
   claimed hourly and a 1-hour RPO because it described the systemd timer. The
   two cadences must both be stated, per deployment, or the reader cannot tell
   which one applies to them.
2. **A manual backup must not be run via `--entrypoint sh`.** That bypasses the
   `apk add bash sqlite` in the service's `command:` and fails with
   `env: can't execute 'bash': No such file or directory` (rc 127). The runbook
   uses `docker compose exec backup /scripts/backup.sh /backups`.

- [ ] **Step 4: Verify the restore actually works**

Run against a stack that has rows worth losing (`docker compose run --rm api
seed`, then sign in through `/v1/auth/otp/*` and upload a photo). **Pin `STAMP`
to a snapshot taken before the last sign-in.** `ls -t | head -1` picks the
*newest* snapshot, which is normally the one taken most recently — and restoring
it returns a count equal to the live one, so the drill proves nothing. Restoring
an older snapshot must visibly *lose* the rows written after it.

```bash
docker compose stop api
STAMP=backups/gamematch-<older-stamp>.db
sqlite3 "$STAMP" 'PRAGMA integrity_check;'                      # ok
sqlite3 "$STAMP" 'SELECT count(*) FROM users;' > /tmp/opencode/before.txt
sudo mv data/gamematch.db data/gamematch.db.pre-restore
sudo rm -f data/gamematch.db-wal data/gamematch.db-shm
sudo cp "$STAMP" data/gamematch.db
# GUARDED: data/photos is absent on a fresh stack, so the archive is
# legitimately missing and an unconditional tar exits 2 AFTER the database
# has already been replaced. See "Five things" below.
[ -f "backups/photos-<stamp>.tar.gz" ] && sudo tar xzf "backups/photos-<stamp>.tar.gz" -C data
sudo chown -R 10001:10001 data
docker compose start api
sleep 4
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/v1/healthz
sqlite3 data/gamematch.db 'SELECT count(*) FROM users;' > /tmp/opencode/after.txt
diff /tmp/opencode/before.txt /tmp/opencode/after.txt && echo "restore verified"
sudo rm -rf data/gamematch.db.pre-restore data/photos.pre-restore   # once verified
```

Expected: `200`, and `restore verified` — identical counts.

Five things about this block as written above, all found during execution:

- **`mv`, `cp` and `chown` inside `./data` need root.** `./data` is mode `0755`
  owned by UID 10001, so a non-root deploy user is refused with
  `Permission denied`. `sudo` is in the commands for that reason; the runbook
  also records the throwaway-root-container equivalent. **The systemd procedure
  in `restore.md` had the same omission on `/var/lib/gamematch` and was fixed in
  the same pass.**
- **`sqlite3 data/gamematch.db …` after `stop api` fails** with
  `attempt to write a readonly database (8)`. `sqlite3` opens read-write, a WAL
  database needs its `-shm`, and a clean stop *deleted* that sidecar — so
  SQLite tries to create one in a directory the host user does not own. Verify
  as root, after `docker compose start api` has recreated the sidecars, or from
  the `backup` container, which answers either way.
- **`diff` must compare like with like.** Redirecting the
  `PRAGMA integrity_check` output into `before.txt` alongside the user count
  makes the diff compare a two-line file with a one-line file, and it fails on a
  restore that worked. Let the integrity check print to the terminal.
- **The photo archive has to be guarded and the live tree moved aside.** An
  unconditional `tar` exits 2 on a fresh stack, after the database has been
  replaced. Extracting over a live `data/photos` leaves every photo uploaded
  *after* the snapshot as an orphan no database row references.
- **The aside files have to be deleted afterwards.**
  `gamematch.db.pre-restore` is a complete stale copy of the pre-restore
  database, inside the directory the backup service archives from, and nothing
  prunes it.

One more trap, and the reason the drill must not be run by copying a command out
of a stock image: **`alpine:3.24` has no `sqlite3` and no `bash`.** A throwaway
container used to perform the privileged steps needs `apk add --no-cache sqlite`,
and needs `set -e` — `sh -c` has none, so a failed step prints `sqlite3: not
found`, exits 127, and the rest of the transcript is published anyway. This
produced a plausible-looking `live users before restore: 6` line under a heading
asserting the output was real, when no `sqlite3` had existed to print it. A
restore transcript that cannot have come from its own command is worse than no
transcript.

The raw output of the drill, including all five failures above, is in
`docs/runbooks/deploy-docker.md` Appendix B.3.

- [ ] **Step 5: Commit**

```bash
git add docs/runbooks/deploy-docker.md docs/runbooks/deploy.md docs/runbooks/restore.md
git commit -m "docs: Docker runbook, with a verified local walkthrough

Records the actual output of the local verification rather than a
paraphrase, so the VPS deploy has a known-good reference. Restore steps
now stop the API container first, so no live process rewrites the WAL
sidecars mid-restore."
```

---

## Task dependency map

```
Task 1 (Caddyfile)      ─┐
                          ├─> Task 3 (compose) ─> Task 5 (e2e) ─> Task 6 (prod) ─> Task 7 (docs)
Task 2 (images)         ─┘         │
                                    └─> Task 4 (backup) ──┘
```

Tasks 1 and 2 are independent and can run in either order. Task 3 needs both. Tasks 4 and 5 need 3. Tasks 6 and 7 need 5.
