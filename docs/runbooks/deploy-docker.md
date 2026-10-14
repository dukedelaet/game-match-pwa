# Docker deployment

> **Status: this is the supported deployment path for GameMatch.** One compose
> file, three services, one `.env`. `docs/runbooks/deploy.md` documents the older
> systemd deployment and is kept for hosts already running it — do not start a
> new VPS from it.
>
> Every command in this document is copy-pasteable. Every block labelled
> **Verified** is real terminal output captured against this stack on 2026-10-14,
> not a paraphrase; the evidence is in [Appendix A](#appendix-a--verification-evidence)
> and [Appendix B](#appendix-b--verification-evidence-continued).

## Contents

- [What runs](#what-runs)
- [Deploying to a VPS](#deploying-to-a-vps)
- [Verifying the deploy](#verifying-the-deploy)
- [Updating a deployment](#updating-a-deployment)
- [Backups](#backups)
- [Restoring](#restoring)
- [Troubleshooting](#troubleshooting)
- [Appendix A — verification evidence](#appendix-a--verification-evidence)
- [Appendix B — verification evidence (continued)](#appendix-b--verification-evidence-continued)

## What runs

Three services, defined in `docker-compose.yml`:

| Service | Image | Exposed | Does |
| --- | --- | --- | --- |
| `api` | `gamematch-api:latest` | **nothing** — no `ports:` mapping, reachable only on the compose network | The Go binary. SQLite at `/data/gamematch.db`, photos at `/data/photos`, UID 10001. |
| `web` | `gamematch-web:latest` | `80`, `443` (from `HTTP_PORT`/`HTTPS_PORT`) | Caddy. Terminates TLS, serves `apps/web/dist`, reverse-proxies `/v1` to `api:8000`. |
| `backup` | `alpine:3.24` | nothing | Runs the existing `scripts/backup.sh` once on start and then every 6 hours. |

```
browser ──TLS──> Caddy (web) ─┬── /v1/* ──> api:8000
                               └── /*    ──> apps/web/dist
                                        ▲
host ./data  ──bind mount──> /data in api and backup
host ./backups ──bind mount──> /backups in backup
```

Two things follow from that diagram and both are load-bearing:

- **The `api` service must never publish a port.** It binds `0.0.0.0` inside its
  container because a container's loopback is its own — Caddy is a different
  container and could never reach `127.0.0.1:8000` there. The exposure that
  creates is contained by *not* publishing it. A `ports:` line under `api` would
  put the API on the host's public interface with no TLS and none of the security
  headers in `deploy/Caddyfile`.
- **All durable state is on the host**, in `./data` and `./backups`. The three
  named volumes (`caddy-data`, `caddy-config`, `caddy-logs`) hold only Caddy's
  certificates, autosaved config and access log. `docker compose down` keeps
  everything on the host; `docker compose down -v` deletes the certificates —
  **do not use `-v`**, see [Backups](#backups).

---

## Deploying to a VPS

### Before you start

You need all of these before the first `docker compose up`:

- **A host with Docker Engine and the Compose plugin.** `docker compose version`
  must work. This runbook uses the `docker compose` (v2) syntax throughout.
- **git**, and outbound network access to GitHub and to the Docker Hub.
- **Node and pnpm**, to build the PWA. `mise.toml` pins Node 24 and pnpm 12.9.1
  at the repo root, so `mise install` is the shortest way to get the versions
  this repo was built with. `apps/web/Dockerfile` copies `apps/web/dist/` and
  **builds nothing** — see [Step 5](#5-build-the-pwa-on-the-host).
- **A domain, with DNS already pointing at this host's public IP.** This is the
  one prerequisite that is easy to get wrong and produces the most confusing
  failure. Caddy obtains its TLS certificate on first start, so the A/AAAA record
  must resolve to this host *before* you start the stack. Point the record at the
  host's IPv4 now, and wait for it to propagate. Confirm:

  ```bash
  getent hosts your.domain        # must print this host's public IP
  ```

  If it prints nothing, stop and wait. Registrars and resolvers cache, and this
  can take up to an hour. Caddy does keep retrying in the background — it will
  pick the certificate up once DNS resolves, without a restart — but the site is
  completely unusable until it does: `:80` answers `308` to an HTTPS that cannot
  complete, and there is no plain-HTTP fallback. See the certificate row in
  [Troubleshooting](#troubleshooting).
- **Inbound TCP ports 80 and 443 open** in the host firewall and any security
  group in front of it. Both are published by the `web` service. TCP 443/UDP is
  optional (Caddy will offer HTTP/3); TCP 80 is required even if you never use
  plain HTTP, because Caddy answers it with a redirect to HTTPS.
- **Outbound TCP 443 to the alpine package mirrors.** The `backup` container runs
  `apk add --no-cache bash sqlite` on *every* start, not at build time. A host
  that blocks it gets a crash-restarting backup container and no backups.

### 1. Get the code

```bash
git clone https://github.com/dukedelaet/game-match-pwa.git
cd game-match-pwa
```

### 2. Create `.env`

```bash
cp .env.example .env
```

`.env` is gitignored. `.env.example` is the committed template and holds **two
complete configurations** — a local one and a VPS one — so there is nothing else
to look up. `.env` is the only file you change between a laptop and a server.

### 3. Fill in the production variables

**Do not hand-edit `APP_ENV` and leave the rest of the local block in place.**
That is the single most common way this deploy fails, and it fails on the first
boot rather than halfway through the deploy. `.env.example`'s own instructions
(lines 7–8) are: *comment out every line of the local block, then uncomment every
line of the VPS block except `DEV_OTP`.* Here that is as two `sed` commands:

```bash
# 1. comment out every active line — that is the whole local block
sed -i 's|^\([A-Z][A-Z_0-9]*=\)|#\1|' .env

# 2. uncomment the VPS block, leaving DEV_OTP and the optional web-push block alone
sed -i '/^# --- VPS:/,/^# --- optional:/{/^#DEV_OTP=/!{/^#[A-Z][A-Z_0-9]*=/s/^#//}}' .env
```

The result:

```
# --- local: this machine (APP_ENV=local) -----------------------------------
#GAMEMATCH_SITE=localhost
#HTTP_PORT=80
#HTTPS_PORT=443
#APP_ENV=local
#APP_DEBUG=true
#DEV_OTP=123456
#HOUSE_USER_ID=00000000-0000-4000-8000-000000000001
...
# --- VPS: public host (APP_ENV=production) ----------------------------------
APP_ENV=production
GAMEMATCH_SITE=gamematch.example.com
HTTP_PORT=80
HTTPS_PORT=443
APP_DEBUG=false
#DEV_OTP=
SMS_PROVIDER=twilio
TWILIO_ACCOUNT_SID=
TWILIO_AUTH_TOKEN=
TWILIO_FROM=
TURNSTILE_SECRET=
HOUSE_USER_ID=00000000-0000-4000-8000-000000000001
```

Two details in that `sed` are worth stating because getting them wrong is what
breaks the boot:

- **The delimiter is `|`, not `/`.** `sed 's/^X=/\/#\1/'` does *not* insert a
  `#` — GNU sed treats `\#` specially and you get a literal `/#` on every line.
  Verified:
  ```
  $ printf 'A=1\nB=2\n' | sed 's/^\([A-Z][A-Z_0-9]*=\)/\/#\1/'
  /#A=1
  /#B=2
  $ printf 'A=1\nB=2\n' | sed 's|^\([A-Z][A-Z_0-9]*=\)|#\1|'
  #A=1
  #B=2
  ```
- **`DEV_OTP` stays commented.** `.env.example` ships `DEV_OTP=123456`
  *uncommented* in the local block, and `Config.Validate()` refuses to boot a
  production process that has any `DEV_OTP` at all
  (`apps/server/internal/config/config.go:126-128`). If you only flip `APP_ENV`
  instead of swapping blocks, the API crash-loops with
  `config: unsafe production configuration: DEV_OTP` and the site shows a 502
  on every API call. See the `DEV_OTP` row in [Troubleshooting](#troubleshooting).

Now edit the values. `nano .env` (or your editor of choice):

| Line | What to put in it |
| --- | --- |
| `GAMEMATCH_SITE` | **Your real domain.** Left as `gamematch.example.com`, Caddy requests a certificate for a domain you do not own and every visitor gets a name mismatch. |
| `TWILIO_ACCOUNT_SID` | From the Twilio console. Must start with `AC`. |
| `TWILIO_AUTH_TOKEN` | From the Twilio console. |
| `TWILIO_FROM` | Your Twilio number in E.164 form, e.g. `+15550000000`. |
| `TURNSTILE_SECRET` | The **secret** key from Cloudflare Turnstile, not the site key. |
| `VAPID_*` (optional) | Only if you want web push. Generate a pair with `docker compose run --rm api vapid`. |

`APP_ENV=production` is the guardrail that matters most: outside production,
`Config.Validate()` returns immediately, so a VPS configured with a real domain
but without it starts happily, serves a real Let's Encrypt certificate, and then
**every sign-in fails silently** — codes are generated and thrown away instead of
being sent. With `APP_ENV=production` set, the server refuses to boot and *names*
each variable it is missing, so you can fill them in at your own pace.

Check the file before going further. Three checks, each with one unambiguous
expected result:

```bash
# 1. no key is set twice — no output means each appears exactly once
grep -E '^[A-Z]' .env | cut -d= -f1 | sort | uniq -d

# 2. no key is left empty — no output means every value is filled in
grep -E '^[A-Z][A-Z_0-9]*=$' .env

# 3. DEV_OTP is absent — no output means it is not set at all
grep -E '^DEV_OTP=' .env
```

**Edit the values in place, in the file, with an editor.** Appending a corrected
value does work — Compose's `env_file` takes the last occurrence, which is how the
placeholder credentials below were set up — but it leaves the empty original on
the line above. That produces a file where the two blocks disagree on the same
key, which is the same class of mistake as the `DEV_OTP` one that crash-loops the
API, and it makes check 1 above permanently red. Verified, for the record:

```
$ printf 'TWILIO_ACCOUNT_SID=ACplaceholder\n' >> .env
$ grep -E '^[A-Z]' .env | cut -d= -f1 | sort | uniq -d
TWILIO_ACCOUNT_SID
```

The container then gets the appended value — also verified, `docker compose exec
api env | grep TWILIO_ACCOUNT_SID` printed `TWILIO_ACCOUNT_SID=ACplaceholder` — so
the deploy works and the file is now ambiguous. Fill the value in on the existing
line instead.

Check 1 compares **keys**, not whole lines, which is the whole point: the duplicate
that matters is `TWILIO_ACCOUNT_SID=` followed by `TWILIO_ACCOUNT_SID=ACplaceholder`,
two different lines. `sort | uniq -d` on whole lines does not see that, and a
duplicated key with two identical values is found by accident rather than by
design.

### 4. Create the data and backups directories

```bash
mkdir -p data backups
sudo chown -R 10001:10001 data
```

The API container runs as **UID 10001**, non-root. It cannot write a database
into a host directory it does not own, and the failure names neither the path nor
the UID. `./backups` stays owned by your deploy user: the `backup` container runs
as root, so it can write there regardless.

If you would rather not use `sudo`, a throwaway root container does the same
thing and needs no host credential:

```bash
docker run --rm -v "$PWD/data:/data" alpine:3.24 chown -R 10001:10001 /data
```

Check it, before the first start:

```bash
ls -ldn data          # 10001 10001
```

### 5. Build the PWA on the host

```bash
pnpm install
pnpm --filter web build
```

**This step is required and is not optional.** `apps/web/Dockerfile:21` is
`COPY apps/web/dist/ /srv/dist/` and the web image builds nothing. `apps/web/dist`
is gitignored, so on a fresh clone it does not exist and the build below fails:

```
$ docker compose build web
#9 ERROR: failed to calculate checksum of ref ttzdq1zuskb4fffb3vjrx7fvr::k5yikmgyagrsntt5zt60fxwg9: "/apps/web/dist": not found
 > [2/3] COPY apps/web/dist/ /srv/dist/:
Dockerfile:21
failed to solve: failed to compute cache key: failed to calculate checksum of ref ttzdq1zuskb4fffb3vjrx7fvr::k5yikmgyagrsntt5zt60fxwg9: "/apps/web/dist": not found
```

This step is also the frontend deploy. The image ships whatever `dist/` contained
when you built it — a PWA change is `pnpm --filter web build` followed by a
`web` restart, with no API rebuild.

### 6. Start the stack

```bash
docker compose up --build -d
```

```
 Volume gamematch_caddy-logs Creating
 Volume gamematch_caddy-data Creating
 Volume gamematch_caddy-config Created
 Volume gamematch_caddy-logs Created
 Volume gamematch_caddy-data Created
 Network gamematch_default Created
 Container gamematch-api-1 Creating
 Container gamematch-api-1 Created
 Container gamematch-web-1 Creating
 Container gamematch-backup-1 Creating
 Container gamematch-web-1 Created
 Container gamematch-backup-1 Created
 Container gamematch-api-1 Starting
 Container gamematch-api-1 Started
 Container gamematch-api-1 Waiting
 Container gamematch-backup-1 Starting
 Container gamematch-backup-1 Started
 Container gamematch-api-1 Healthy
 Container gamematch-web-1 Starting
 Container gamematch-web-1 Started
```

(Docker interleaves each progress line twice on a TTY; the duplicates are dropped
above. Nothing else is.)

`api` goes `Healthy` before `web` starts (`web` declares
`depends_on: api: condition: service_healthy`), so by the time Caddy is listening
the proxy upstream exists.

**Local development hosts only — skip this on a VPS.** `seed` inserts demo
accounts (Alex, Jordan, Staff) with fixed phone numbers and a known dev OTP. On a
public host you do not want them:

```bash
# your laptop
docker compose run --rm api seed
2026/10/14 22:48:33 seeded /data/gamematch.db
```

On a VPS, skip it: the command is optional and skipping it just means a fresh,
empty database. Real users sign themselves up. (`seed` is idempotent — re-running
it re-seeds over an existing database without complaint — so a demo row that does
get created on a production host can be removed by deleting that user.)

`deploy.md:88` says the same thing for the systemd path: *demo data is for local
hosts only*.

### 7. First things to check

```bash
docker compose ps
docker inspect --format '{{.RestartCount}}' gamematch-api-1     # must be 0
docker compose logs api | tail -5
```

```
NAME                 IMAGE                  COMMAND                  SERVICE   CREATED          STATUS                    PORTS
gamematch-api-1      gamematch-api:latest   "/usr/local/bin/game…"   api       24 seconds ago   Up 14 seconds (healthy)   8000/tcp
gamematch-backup-1   alpine:3.24            "sh -c 'apk add --no…"   backup    6 minutes ago    Up 6 minutes (healthy)    
gamematch-web-1      gamematch-web:latest   "caddy run --config …"   web       19 seconds ago   Up 10 seconds             0.0.0.0:80->80/tcp, [::]:80->80/tcp, 0.0.0.0:443->443/tcp, [::]:443->443/tcp, 443/udp, 2019/tcp

$ docker inspect --format '{{.RestartCount}}' gamematch-api-1
0

$ docker compose logs api | tail -3
api-1  | 2026/10/14 22:34:28 gamematch listening on http://0.0.0.0:8000 (data dir /data, debug=false)
api-1  | 2026/10/14 22:34:30 GET /v1/healthz 200 0s
```

Three things to read off that:

1. **`RestartCount` is 0.** This — not `docker compose ps` and not
   `.State.ExitCode` — is the check that cannot be misread. See
   [Troubleshooting](#troubleshooting).
2. **`debug=false`** in the boot line. `APP_ENV=local` would print `debug=true`,
   and `APP_DEBUG=true` in production makes every one-time code the constant
   `123456` and returns it in the API response, so anyone who can reach the site
   can sign in as any phone number.
3. **`backup` shows `health: starting` for the first few minutes and that is
   normal.** It runs `apk add`, then `sleep 30`, then takes its first backup. The
   healthcheck has a 180s start period for exactly this. Confirm it lands:

   ```bash
   ls -l backups/
   docker compose ps backup
   ```

   ```
   -rw-r--r-- 1 root root 516096 Oct 14 22:29 gamematch-20261014T222905Z.db
   -rw-r--r-- 1 root root   8984 Oct 14 22:29 photos-20261014T222905Z.tar.gz
   ```

   A `.db` and, once photos exist, a `photos-*.tar.gz`. See
   [Backups](#backups) for what a missing photos archive means.

Then, from outside:

```bash
curl -fsS https://your.domain/v1/healthz      # {"ok":true}
curl -fsSI https://your.domain/boot | head -1 # HTTP/2 200
```

And confirm the API is *not* reachable on the host, which is the property the
whole design exists to protect:

```bash
docker compose ps --format '{{.Service}} {{.Ports}}'
curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:8000/v1/healthz
```

```
$ docker compose ps --format '{{.Service}} {{.Ports}}'
api 8000/tcp
backup
web 0.0.0.0:80->80/tcp, [::]:80->80/tcp, 0.0.0.0:443->443/tcp, [::]:443->443/tcp, 443/udp, 2019/tcp

$ curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:8000/v1/healthz
000

$ docker compose exec web wget -qO- http://api:8000/v1/healthz
{"ok":true}
```

`000` is connection refused. The `wget` from inside the `web` container is there
so that `000` cannot be mistaken for a dead API — the API is reachable, just not
from outside the compose network. If the host-level probe ever returns `200`,
something has added a `ports:` mapping to the `api` service — stop and fix it
before the host is exposed to the internet.

Finally, **try signing in from a real phone.** That is the only check that
exercises Twilio and Turnstile together, and it is the one that catches wrong
credentials — the API answers `{"ok":true}` and logs the send failure rather
than surfacing it to the user.

---

## Verifying the deploy

`docker compose ps` output and the checks that can be run against a live stack
are in [Appendix A](#appendix-a--verification-evidence). The short form:

| Check | Command | Expected |
| --- | --- | --- |
| API is up and staying up | `docker inspect --format '{{.RestartCount}}' gamematch-api-1` | `0` |
| API reached through TLS | `curl -fsS https://your.domain/v1/healthz` | `{"ok":true}` |
| PWA served | `curl -fsSI https://your.domain/boot \| head -1` | `HTTP/2 200` |
| SPA fallback works | `curl -fsS -o /dev/null -w '%{http_code}\n' https://your.domain/deep/spa/route` | `200` |
| Security headers present | see Appendix A step 6 | `1` each |
| API not exposed on the host | `curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:8000/v1/healthz` | `000` |
| Data directory ownership | `docker compose exec api ls -ldn /data` | `10001 10001` |
| WAL is actually in use | `docker compose exec backup sqlite3 /data/gamematch.db 'PRAGMA journal_mode;'` | `wal` |
| Backups are landing | `ls -l backups/` | a new `.db` every 6h |

---

## Updating a deployment

```bash
git pull
pnpm --filter web build        # only if the frontend changed
docker compose up --build -d
```

- A frontend-only change: `pnpm --filter web build && docker compose up -d --build web`.
  The web image builds nothing; it copies `dist/`.
- An API change: `docker compose up -d --build api`. `web` needs no restart — it
  reverse-proxies to the service name `api:8000`, which Docker's embedded DNS
  re-resolves when the container is recreated, so the new binary is picked up by
  the next request.
- A configuration change: edit `.env`, then `docker compose up -d --force-recreate
  api`. Compose only recreates a container when the file's *hash* changes, so a
  `sed` that edits a value Compose does not track (or one it tracks under a
  different name) needs the explicit `--force-recreate`.

Migrations run automatically on boot from the embedded set, keyed on
`PRAGMA user_version`. There is no migration step to remember, and no way to run
one without starting the service.

Rolling back a bad deploy is `git checkout <previous-sha>`, rebuild, `up -d`.
The schema does not go backwards, so roll *forward* with a new migration rather
than expecting an old binary to run against a newer schema.

---

## Backups

The `backup` service runs the existing `scripts/backup.sh`: **once shortly after
start, and then every 6 hours** (`sleep 21600` plus the duration of the run
itself). Each run writes `gamematch-<stamp>.db` and, when `data/photos` exists,
`photos-<stamp>.tar.gz` into `./backups`, and prunes anything older than 14 days.

| Property | Value |
| --- | --- |
| Interval | 6 hours (not hourly — that is the systemd timer in `deploy.md`) |
| Recovery point objective | **up to 6 hours** of lost writes, plus the duration of a run |
| Retention | 14 days (`-mtime +14`), so roughly 56 snapshots |
| Health signal | `backup` reports `unhealthy` when the newest `.db` is more than 7 hours old |
| Location | `./backups` on the host — **the same disk as the database** |

Two operational consequences:

- **`./backups` is on the same host as `./data`.** A disk failure takes both.
  Copy them off-box (`rsync`, `restic`, whatever you already run) or the backups
  buy you nothing against the failure they exist for.
- **Do not run `docker compose down -v`.** It does not touch `./data` or
  `./backups` — both are bind mounts — but it *does* delete `caddy-data`, the
  ACME account and every certificate. The next `up` re-requests them, and Let's
  Encrypt permits 5 duplicate certificates per week per registered domain, which
  is enough to lock the domain out for the rest of the week. Plain
  `docker compose down` is safe.

Take a backup on demand, without waiting for the schedule:

```bash
docker compose exec backup /scripts/backup.sh /backups
```

```
backup: wrote /backups/gamematch-20261014T222944Z.db
```

Run it through the **service's own entry point**. Overriding the entry point
replaces the service's `command:` — which is where `apk add bash sqlite` lives —
and the script's `#!/usr/bin/env bash` shebang then has nothing to run:

```
$ docker compose run --rm --entrypoint /scripts/backup.sh backup /backups
env: can't execute 'bash': No such file or directory
rc=127
```

To take a snapshot from a stopped stack instead, start the stack first — the
backup container is what has `sqlite3` on it.

**A missing `photos-*.tar.gz` is the failure to watch for.** `scripts/backup.sh`
guards the archive with `if [ -d "$DATA_DIR/photos" ]` and otherwise does
nothing at all: no warning, exit 0, a valid `.db` written as usual, and the
container stays `healthy`, because the healthcheck only looks at `.db` files. If
the photos directory is ever deleted, restored from the wrong place, or left
unmounted by a bind-mount typo, **every subsequent backup is silently
database-only, every 6 hours, indefinitely.** The database keeps its photo rows,
so nothing in the app breaks; the loss only surfaces at the point of recovery.

`data/photos` does not exist on a fresh stack and that is benign — it is created
by the first photo upload. What is *not* benign is it disappearing later. Check
it alongside your backup listing.

---

## Restoring

Full procedure, with the reasoning it rests on, in
[`restore.md`](restore.md). The shape of it, for the compose stack:

```bash
docker compose stop api          # nothing may write while you restore
sudo mv data/gamematch.db data/gamematch.db.pre-restore
sudo rm -f data/gamematch.db-wal data/gamematch.db-shm
sudo mv data/photos data/photos.pre-restore      # only if there is an archive below
sudo cp backups/gamematch-<stamp>.db data/gamematch.db
sudo tar xzf backups/photos-<stamp>.tar.gz -C data   # guard it — see restore.md
sudo chown -R 10001:10001 data
docker compose start api
sudo rm -rf data/gamematch.db.pre-restore data/photos.pre-restore   # after verifying
```

Five things about that sequence that are specific to this stack:

1. **Stop the API first.** A live process rewrites the WAL sidecars mid-restore,
   and a `-wal` left beside a freshly restored database is replayed onto it.
2. **`sudo` on everything that touches `./data`.** That directory is mode `0755`
   owned by UID 10001, so the deploy user cannot create or rename an entry in it.
3. **`chown -R 10001:10001 data`, not just the `.db`.** The extracted photos
   directory comes back root-owned unless you do.
4. **The photo archive is guarded.** `data/photos` does not exist on a fresh
   stack, so the archive is legitimately absent and `tar` would exit 2 *after* the
   database had already been replaced. `restore.md` wraps it in a test.
5. **Verify as root, or after the API is back up.** `sqlite3 <file> 'SELECT ...'`
   opens read-write by default, and with the API stopped there is no `-shm` side
   car for it to use, so a plain host-side query fails:
   ```
   $ sqlite3 data/gamematch.db 'SELECT count(*) FROM users;'
   Parse error in 2nd command line argument: attempt to write a readonly database (8)
   ```
   Once `docker compose start api` has run, the sidecars exist and the same
   query works. Or ask the `backup` container, which has `sqlite3` and the same
   bind mount and answers either way:
   ```
   $ docker compose exec backup sqlite3 /data/gamematch.db 'PRAGMA journal_mode;'
   wal
   ```

The last two lines — deleting the `.pre-restore` copies — are not optional
tidiness. `gamematch.db.pre-restore` is a **complete stale copy of the database**,
inside the directory the backup service archives from, and nothing prunes it.
Keep the aside files only for as long as you might still need to roll back.

---

## Troubleshooting

Each row is a failure this stack actually produces, with the evidence behind it
in the appendix.

| Symptom | Cause | How to confirm | Fix |
| --- | --- | --- | --- |
| **The PWA loads but every API call is 502.** The site "works" — `/` returns `200` — and the whole backend is down. | The `api` container is crash-looping on `Config.Validate()`. Because of `restart: unless-stopped` it never rests in `Exited`, so a single `docker compose ps` sample usually reads `Up 1 second (health: starting)`, which looks like a normal start. | `docker compose logs api \| tail -20` — the message **names the missing variable**. `docker inspect --format '{{.RestartCount}}' gamematch-api-1` is non-zero and climbing. | Fill in the variable it names and `docker compose up -d --force-recreate api`. See the `.env` section. |
| **`config: unsafe production configuration: DEV_OTP`** | `.env.example`'s local block ships `DEV_OTP=123456` uncommented. Switching only `APP_ENV` leaves it set, and `Validate()` refuses any non-empty `DEV_OTP` in production (`internal/config/config.go:126-128`). | `docker compose logs api \| tail -5` | Do not hand-edit `APP_ENV`. Use the two `sed` commands in [step 3](#3-fill-in-the-production-variables), which comment the whole local block out. |
| **Caddy's log says `server misbehaving`, not `connection refused`.** | Docker's embedded DNS failing to resolve `api`, because the `api` container is being torn down and recreated constantly. Editing `GAMEMATCH_API` will fix nothing. | `docker compose logs web \| grep 'server misbehaving'` — the same signature as the row above, and a different cause. | Fix the crash loop above. |
| **`docker compose ps` says `Up 1 second (health: starting)` but the service is dying.** | A sample of a crash loop, caught mid-cycle. | Sample it repeatedly, or read `RestartCount`. | Read the log. |
| **You reach for `.State.ExitCode` to check a crash and it reads `0`.** | It resets to `0` on every restart, so during a crash loop it reads `0` once per cycle, in the window before `Validate()` kills the process again. On a healthy boot it is also `0`, so the two cases are indistinguishable — a worse misread than `ps`, because `0` looks definitive. | Measured: 7 of the first 50 samples were `0` during a crash loop. | Use `docker inspect --format '{{.RestartCount}}' gamematch-api-1`. Monotonic, and non-zero after any failed deploy. |
| **Permission denied writing the database; the API dies on boot.** | `./data` is not owned by UID 10001. The image runs non-root and the message names neither the host path nor the UID. | `ls -ldn data`; `docker compose exec api ls -ldn /data` — both must be `10001 10001` | `sudo chown -R 10001:10001 data` |
| **`curl https://your.domain/…` fails with `curl: (35) TLS connect error`.** Plain HTTP still answers `308 Permanent Redirect` to an HTTPS that cannot complete, so "the redirect works" is not a good sign. | Caddy could not obtain a certificate — usually because DNS was pointed at the host *after* the first start, or 80/443 is firewalled, or `GAMEMATCH_SITE` is still the `gamematch.example.com` placeholder. | `docker compose logs web \| grep tls.obtain` | Point the A/AAAA record at this host and **confirm it resolves from here** (`getent hosts your.domain`) before restarting. Caddy retries on its own — `will retry … retrying_in: 60 … max_duration: 2592000`, i.e. 30 days — so `docker compose restart web` will pick it up immediately but is not required once DNS resolves. On a real domain the CA's error differs: a name that does not resolve fails at the HTTP-01 challenge step, not at order creation. |
| **`docker compose build web` fails with `"/apps/web/dist": not found`.** | `apps/web/dist` is gitignored and the web image copies it without building it. | The error names the `COPY` line. | `pnpm install && pnpm --filter web build`, then rebuild. |
| **Every call through Caddy 502s, but the API container is healthy.** | `ADDR` was set to `127.0.0.1:8000` — from a stray `ADDR=` line in `.env`. Container loopback is not reachable from a sibling container. | `docker compose logs api \| head -1` — it will say `listening on http://127.0.0.1:8000`, not `0.0.0.0:8000` | Remove the `ADDR` line. The image sets `ENV ADDR=0.0.0.0:8000`; do not add one to `.env`. |
| **The database will not open, or `backup` fails with `disk I/O error` / `unable to open database file`.** | SQLite runs in WAL mode, which needs a filesystem with working POSIX locking. Some VPS overlay and network filesystems do not have it. The service can start and then fail on first write. | `docker compose exec backup sqlite3 /data/gamematch.db 'PRAGMA journal_mode;'` — `wal` means locking works. Run it **in the `backup` container, not on the host**: `sqlite3` opens read-write by default and, with the API stopped, there is no `-shm` sidecar for it to use, so the host-side form fails with `attempt to write a readonly database (8)` — the same trap as [Restoring](#restoring). The `backup` container has `sqlite3` and the same bind mount, and answers either way. | Put `./data` on local disk (ext4, xfs, btrfs), not on NFS, CIFS, or a fuse mount. Relocating it is a stop, `mv`, restart. |
| **Nobody can sign in, and the response is `{"ok":true}` with no `devCode`.** | `otp/start` enforces its rate limits **silently**: over the limit it stores nothing and says nothing else, on purpose, so the endpoint cannot be used to probe which numbers exist. Limits are 5 per IP and 3 per phone per 15 minutes and they are **persisted**, so restarting `api` does not clear them. | `sqlite3 data/gamematch.db "SELECT bucket, count FROM rate_limits WHERE bucket LIKE 'otp:start%';"` — with `APP_DEBUG=true`, a code that *was* issued comes back as `devCode`, so its absence is the signal. | Wait the window out, or clear the rows. Do not conclude the configuration is wrong. |
| **`sms send: twilio: status 401: Authentication Error - invalid username`.** | Wrong `TWILIO_ACCOUNT_SID`/`TWILIO_AUTH_TOKEN`. The request still returns `{"ok":true}`. | `docker compose logs api \| grep "sms send"` | Fix the credentials and `docker compose up -d --force-recreate api`. |
| **No `photos-*.tar.gz` across several consecutive backups.** | `data/photos` is gone. The script skips the archive without warning, exits 0, and the backup container stays `healthy` because its healthcheck only looks at `.db` files. | `ls -ld data/photos`; compare consecutive `ls backups/` listings | Restore the directory. Until then every backup is database-only — see [Backups](#backups). |
| **The `backup` container restarts over and over; `docker compose logs backup` is nearly empty.** | Its `command:` runs `apk add --no-cache bash sqlite` on every start. If the host cannot reach the alpine mirrors, PID 1 exits non-zero and `restart: unless-stopped` retries with backoff for as long as the stack is up. | `docker compose logs backup` | Open outbound network access to the alpine package mirrors. |
| **`docker compose exec web caddy reload` → `dial tcp [::1]:2019: connect: connection refused`.** | `deploy/Caddyfile:21` is `admin off`, so the admin API on :2019 is not listening. `reload` is implemented on top of it. | `docker compose exec web caddy adapt --config /etc/caddy/Caddyfile --pretty` shows `"admin": {"disabled": true}` | To check a config: `caddy adapt` (offline, touches nothing). To apply one: `docker compose restart web`. A restart drops in-flight TLS connections — `stop_grace_period: 30s` lets Caddy drain first, but a client mid-request at that instant is cut. |
| **Manual backup fails with `env: can't execute 'bash': No such file or directory` (rc 127).** | You overrode the entry point, which removed the service's `command:` and with it `apk add bash sqlite`. The script's shebang is `#!/usr/bin/env bash` and stock alpine has no bash. | The `rc=127` above. | `docker compose exec backup /scripts/backup.sh /backups`. |

Two general rules that cover most of the above:

- **Read `docker compose logs api`, not `docker compose ps`.** `ps` samples a
  crash loop at an arbitrary point in its cycle and every sample looks plausible.
  The log cannot be misread, and it names the variable.
- **Probe more than one URL.** "The site loads" is not evidence the API booted: a
  crash-looping API still serves the PWA at `/` and 502s `/v1`. A missing
  certificate is the reverse — `/` fails too.

---

## Appendix A — verification evidence

### A1 — Verified on local Docker

Recorded on a local machine on 2026-10-14, against the stack as committed at
this point in the plan (Tasks 1–4). Every command below was run against the live
stack, and every value in the output blocks is the real one — no number, header,
status, path or count has been altered. A few blocks are trimmed to the part
that matters: §1 drops the `Volume gamematch_caddy-config Created` and
`Volume gamematch_caddy-data Created` lines, and `cat jar` omits curl's three
header lines. Each trimmed block says so.

Two things about how to read it:

- Caddy serves HTTPS with its **internal CA**, so `curl -k` appears throughout.
  That is expected; there is no plain-HTTP mode.
- The API container is deliberately **not** published to the host, so it has no
  URL. Everything below reaches it through Caddy on `https://localhost`, or from
  inside the compose network.

#### Prerequisites

```bash
pnpm install
pnpm --filter web build        # REQUIRED — see below
cp .env.example .env           # APP_ENV=local in the local block
mkdir -p data backups
sudo chown -R 10001:10001 data
```

**The PWA build is not optional.** `apps/web/Dockerfile:21` is
`COPY apps/web/dist/ /srv/dist/` and the web image builds nothing — it copies
prebuilt output. `apps/web/dist` is gitignored, so on a fresh clone it does not
exist and the build fails:

```
$ docker compose build web          # apps/web/dist moved aside = fresh clone
#9 ERROR: failed to calculate checksum of ref ttzdq1zuskb4fffb3vjrx7fvr::k5yikmgyagrsntt5zt60fxwg9: "/apps/web/dist": not found
------
 > [2/3] COPY apps/web/dist/ /srv/dist/:
------
Dockerfile:21
failed to solve: failed to compute cache key: failed to calculate checksum of ref ttzdq1zuskb4fffb3vjrx7fvr::k5yikmgyagrsntt5zt60fxwg9: "/apps/web/dist": not found
```

`pnpm --filter web build` from that same state:

```
$ pnpm --filter web build
dist/assets/index-CdqkamKB.css   20.81 kB │ gzip:  4.61 kB
dist/assets/index-DOzqftFo.js   301.71 kB │ gzip: 94.47 kB

✓ built in 200ms
PWA v1.3.0
mode      generateSW
precache  12 entries (331.50 KiB)
files generated
  dist/sw.js
  dist/workbox-2fbc6a65.js
```

**On `./data` ownership:** `sudo` needs a password on the machine this was
recorded on, so `./data` was made uid 10001 with a throwaway root container
instead — same result, no host credential. Use whichever you have:

```bash
mkdir -p data backups
docker run --rm -v "$PWD/data:/data" alpine:3.24 chown -R 10001:10001 /data
```

#### Step 1 — start the stack and seed

```bash
docker compose up -d --build
docker compose run --rm api seed
docker compose ps
```

`up` output, tail — the build steps and the two `Volume gamematch_caddy-data
Created` lines are dropped, nothing else:

```
 Volume gamematch_caddy-logs Created 
 Network gamematch_default Created 
 Network gamematch_default Created 
 Container gamematch-api-1 Creating 
 Container gamematch-api-1 Created 
 Container gamematch-web-1 Creating 
 Container gamematch-backup-1 Creating 
 Container gamematch-web-1 Created 
 Container gamematch-backup-1 Created 
 Container gamematch-api-1 Starting 
 Container gamematch-api-1 Started 
 Container gamematch-api-1 Waiting 
 Container gamematch-backup-1 Starting 
 Container gamematch-backup-1 Started 
 Container gamematch-api-1 Healthy 
 Container gamematch-web-1 Starting 
 Container gamematch-web-1 Started 

$ docker compose run --rm api seed
 Container gamematch-api-run-a05aa2d8c44c Creating
 Container gamematch-api-run-a05aa2d8c44c Created
2026/10/14 21:50:06 seeded /data/gamematch.db

$ docker compose ps
NAME                 IMAGE                  COMMAND                  SERVICE   CREATED          STATUS                             PORTS
gamematch-api-1      gamematch-api:latest   "/usr/local/bin/game…"   api       23 seconds ago   Up 12 seconds (healthy)            8000/tcp
gamematch-backup-1   alpine:3.24            "sh -c 'apk add --no…"   backup    18 seconds ago   Up 10 seconds (health: starting)
gamematch-web-1      gamematch-web:latest   "caddy run --config …"   web       18 seconds ago   Up 8 seconds                       0.0.0.0:80->80/tcp, [::]:80->80/tcp, 0.0.0.0:443->443/tcp, [::]:443->443/tcp, 443/udp, 2019/tcp
```

`api` healthy, `web` up, `backup` up and still `health: starting` — it sleeps
30s, then `apk add`s, then takes its first backup, so healthy is minutes not
seconds on a cold start. Minutes later:

```
$ docker compose ps
NAME                 IMAGE                  COMMAND                  SERVICE   CREATED              STATUS                        PORTS
gamematch-api-1      gamematch-api:latest   "/usr/local/bin/game…"   api       About a minute ago   Up 35 seconds (healthy)       8000/tcp
gamematch-backup-1   alpine:3.24            "sh -c 'apk add --no…"   backup    About a minute ago   Up About a minute (healthy)
gamematch-web-1      gamematch-web:latest   "caddy run --config …"   web       About a minute ago   Up About a minute             0.0.0.0:80->80/tcp, [::]:80->80/tcp, 0.0.0.0:443->443/tcp, [::]:443->443/tcp, 443/udp, 2019/tcp
```

`api seed` is idempotent: it re-seeds over an existing database without
complaint.

#### Step 2 — the API port is NOT published

This is the assertion the whole design exists to protect. If it ever returns
`200`, the API is on the host's public interface with no TLS.

```bash
docker compose ps --format '{{.Service}} {{.Ports}}'
curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:8000/v1/healthz
```

```
$ docker compose ps --format "{{.Service}} {{.Ports}}"
api 8000/tcp
backup
web 0.0.0.0:80->80/tcp, [::]:80->80/tcp, 0.0.0.0:443->443/tcp, [::]:443->443/tcp, 443/udp, 2019/tcp

$ curl -s -o /dev/null -w "%{http_code}\n" --max-time 3 http://127.0.0.1:8000/v1/healthz
000
```

`8000/tcp` with no `->` is the *exposed* port, not a published one — there is no
host half to the arrow. The `000` is connection refused.

Reaching it from inside the network works, so the `000` is containment and not a
dead API:

```
$ docker compose exec web wget -qO- http://api:8000/v1/healthz
{"ok":true}
```

And it is refused on every host interface, not just loopback:

```
$ ss -ltn | grep -c ':8000'
0
$ for ip in 10.194.1.28 100.85.246.117 172.17.0.1 172.18.0.1 172.19.0.1 172.20.0.1; do
    printf '%-16s %s\n' "$ip" "$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://$ip:8000/v1/healthz)"
  done
10.194.1.28      000
100.85.246.117   000
172.17.0.1       000
172.18.0.1       000
172.19.0.1       000
172.20.0.1       000
```

(wlp0s20f0u1i2, tailscale0, docker0 — which is 172.17 — and the three compose
bridges 172.18, 172.19, 172.20. `incusbr0` was not in the loop. Six addresses,
each refused.)

#### Step 3 — the shipped relative-root default is not in use

The defect this catches: with `GAMEMATCH_ROOT` unset and cwd `/`, every asset
404s while `/v1/healthz` still answers 200, so a check that probes only the API
reports success on a site that is down.

```bash
docker compose exec web sh -c 'ls /srv/dist/index.html'
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/assets/$(ls apps/web/dist/assets | grep -m1 '\.js$')
```

```
$ docker compose exec web sh -c 'ls /srv/dist/index.html'
/srv/dist/index.html

$ curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/assets/index-DOzqftFo.js
200
```

The asset name is read from the **host's** `apps/web/dist/assets`, which is the
tree the web image was built from — the running container serves
`/srv/dist/assets/index-DOzqftFo.js`. Reading it inside the container instead
would make the check independent of what is on the host:

```bash
docker compose exec web sh -c 'ls /srv/dist/assets' | grep -m1 '\.js$'
```

#### Step 4 — the proxy reaches the API

```bash
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/v1/healthz
```

```
$ curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/v1/healthz
200
```

`000` here instead would mean Caddy cannot reach `api` — the failure mode if
`ADDR` were left at `127.0.0.1:8000`.

#### Step 5 — the SPA is served with the right caching

```bash
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/boot
curl -skI https://localhost/boot | grep -i cache-control
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/deep/spa/route
```

```
$ curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/boot
200
$ curl -skI https://localhost/boot | grep -i cache-control
cache-control: no-cache
$ curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/deep/spa/route
200
```

`/boot` is the PWA's own `start_url` and is the path the original exact-match
`@shell` allowlist missed.

#### Step 6 — security headers

```bash
for h in Content-Security-Policy Strict-Transport-Security X-Frame-Options Permissions-Policy X-Content-Type-Options; do
  printf '%-32s %s\n' "$h" "$(curl -skI https://localhost/ | grep -ci "^$h:")"
done
```

```
Content-Security-Policy          1
Strict-Transport-Security        1
X-Frame-Options                  1
Permissions-Policy               1
X-Content-Type-Options           1
```

One occurrence each.

#### Step 7 — data survives a restart

The plan's step 7 does not run as written, and **the worst version of it passes
silently**. One query names a column the schema does not have and dies; the
last one is a tautology that exits 0 while asserting nothing. Both are corrected
below, with the literal behaviour of each shown first.

##### (a) `otp_codes` is keyed on `phone`, not `phone_e164` — this one errors

```bash
CODE="$(sqlite3 data/gamematch.db "SELECT code FROM otp_codes WHERE phone_e164 = '+15551239999';")"
```

```
Parse error in 2nd command line argument: no such column: phone_e164
  SELECT code FROM otp_codes WHERE phone_e164 = '+15551239999';
                     error here ---^
could not read the local OTP; is APP_ENV=local?
```

```bash
$ sqlite3 data/gamematch.db ".schema otp_codes"
CREATE TABLE otp_codes (
    phone      TEXT PRIMARY KEY,
    code       TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
```

The step then exits 1 with a message blaming `APP_ENV`, which is set correctly.
Its `test -n "$CODE"` guard also sits *after* the verify call that spends the
code, so an unreadable OTP is only detected after it has been consumed — and
`store.TakeOTP` (`apps/server/internal/store/auth.go:73`) deletes the row as it
reads it, so the row is gone either way.

##### (b) The last query is a tautology — this one silently passes

```sql
SELECT name FROM users WHERE phone_e164_hash =
    (SELECT phone_e164_hash FROM user_private WHERE phone_e164 = '+15551239999');
```

`user_private` has no `phone_e164_hash` column:

```bash
$ sqlite3 data/gamematch.db ".schema user_private"
CREATE TABLE user_private (
    user_id      TEXT PRIMARY KEY,
    phone_e164   TEXT,
    created_at   TEXT,
    updated_at   TEXT
);
```

SQLite resolves an unqualified name innermost-first, so the inner
`phone_e164_hash` does **not** fail — it binds to the outer `users` table. The
subquery becomes *correlated*, and the predicate degenerates to
`users.phone_e164_hash = users.phone_e164_hash`. It is always true (and NULL for
a NULL hash), so it selects **every user with a non-null hash**:

```
$ sqlite3 data/gamematch.db "SELECT name FROM users WHERE phone_e164_hash = (SELECT phone_e164_hash FROM user_private WHERE phone_e164 = '+15551239999');"
Alex
Jordan
Staff
                      <- the new user's NULL name, printed as a blank line
$ echo $?
0
```

Four rows returned, and:

```
$ sqlite3 data/gamematch.db "SELECT count(*) FROM users WHERE phone_e164_hash IS NOT NULL;"
4
```

The result is *all four* non-null-hash users — the predicate is a no-op. The
plan's stated expectation is "the last query printing a name", so `Alex` and
`Jordan` satisfy it and the step reports success **while proving nothing about
the user that just signed in**. That is the same silent-pass as the count
assertion it was written to fix.

The blank fourth line is not a missing name in the output — it *is* the new
user's `name`, which is genuinely NULL: `httpapi/auth.go:192-198` creates the
row with no `Name`, and `store/users.go:71-79` defaults it to
`status=pending, onboarding_step=intent`. Use `quote(u.name)` so NULL is visible
rather than looking like missing output.

##### (c) The corrected step — asserts, and proves *that* user

This is the script the plan now carries, verbatim:

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

Two things changed beyond the column names. The queries **join
`user_private.user_id` to `users.id`**, so they return that one user or nothing —
not every user with a hash. And the identity check asserts with `|| exit 1`
rather than printing two lines for an operator to compare by eye: a count only
proves *some* user row exists, while matching `id` *and* `created_at` across the
restart proves the same row survived.

Note the ordering. `$ID_BEFORE` is captured and asserted **immediately after
the sign-in and before the restart**, so "the sign-in created a row" and "the row
survived the restart" are two separate claims. `$BEFORE`/`$AFTER` straddle only
the restart, so the count is purely a loss check — which is why it is `-ge`:

```
$ bash step7.sh
 Container gamematch-api-1 Restarting 
 Container gamematch-api-1 Started 
before=5 after=5
same row survived: id=225ea504-68ce-4311-b1a4-79647bd89196 created_at=2026-10-14T22:05:10.453223Z
225ea504-68ce-4311-b1a4-79647bd89196|NULL|pending|2026-10-14T22:05:10.453223Z
wal
$ echo $?
0
```

`quote(u.name)` prints `NULL` rather than an empty line, so the absent name is
visible as what it is. `journal_mode` is `wal`, so the restart ran against a live
WAL database on the bind mount — which is the point of the exercise. A host
filesystem that cannot support WAL locking fails here rather than silently later.

With the plan's original `-gt`, the same script re-run fails — the phone is now
registered, so `verify` creates no row and the counts are equal:

```
$ bash step7.sh
 Container gamematch-api-1 Restarting 
 Container gamematch-api-1 Started 
before=5 after=5
FAIL: the user did not survive the restart
$ echo $?
1
```

— the step reporting a lost user when the only thing that happened is that the
user already existed.

##### (c2) `otp/start` fails silently once the rate limit is hit

If `$CODE` comes back empty, the message to believe is **not** `is
APP_ENV=local?`. `otp/start` enforces its limits silently
(`apps/server/internal/httpapi/auth.go:120-125`): over the limit it answers
`{"ok":true}`, stores nothing, and says nothing else — deliberately, so the
endpoint cannot be used to probe which numbers exist. `APP_DEBUG=true` is what
makes it detectable:

```
$ curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15558887777"}'
{"ok":true}            <- no devCode: the limit was hit, nothing was stored
```

With a code issued it is `{"devCode":"123456","ok":true}`. The absence of
`devCode` *is* the signal.

The limits are `limitOTPStartPerIP = 5` and `limitOTPStartPerPhone = 3` per
`windowOTP = 15m` (`apps/server/internal/httpapi/ratelimit.go:16-19`), and they
are **persisted**, so `docker compose restart api` does not clear them:

```
$ sqlite3 data/gamematch.db "SELECT bucket, count, window_start FROM rate_limits WHERE bucket LIKE 'otp:start%';"
otp:start:ip:4be83b3115428950|8|1792015247510
otp:start:phone:834fa3a38e93d341|3|1792015247510
otp:start:phone:c2841877bd1ad1e3|1|1792015264042
otp:start:phone:aab263b3e25bbc25|1|1792015347103
```

8 against a limit of 5. So Step 7 is only reliably runnable a few times per
15 minutes per host, and its failure message blames the wrong thing. Run it with
a number that has not just been used, or wait the window out. On the VPS this is
not a nuisance: it is the same limit that protects Twilio from being burned, and
it is per IP.

##### (d) The corrected query has to be able to say "no"

A check that always returns a row proves nothing either. Same query, same
`JOIN`, for a phone that never signed in:

```
$ sqlite3 data/gamematch.db "SELECT u.id FROM users u JOIN user_private p ON p.user_id = u.id WHERE p.phone_e164 = '+15559990000';"
$ echo $?
0
```

No rows, exit 0 — and `test -n "$ID_BEFORE"` in (c) turns that into a failure.
Note that the tautological version in (b) also returns nothing for this input:
no `user_private` row matches, so the subquery yields NULL and `x = NULL` is
never true. It is therefore not a discriminating control on its own — the
contrast that shows the tautology is the four rows it returns for `$PHONE` in
(b), against one row (the NULL name) from the corrected join.

The seeded users, for reference — `seed` creates **four** named users, and `House`
is the only one with a NULL `phone_e164_hash`, which is why the tautological
query drops it:

```
$ sqlite3 data/gamematch.db "SELECT id, quote(name), phone_e164_hash, status FROM users;"
00000000-0000-4000-8000-000000000001|'House'||active
11111111-1111-4111-8111-111111111130|'Alex'|f5be76815beedc12461645e4a6cf4b031c879dc3758b0b22ffd0fa340c3b9617|active
11111111-1111-4111-8111-111111111131|'Jordan'|3d1ac597aabc6ca5374ca51447cbf3b40c439d3b57cde65f056290a465f7b4b|active
11111111-1111-4111-8111-111111111132|'Staff'|8aacb9d76d462f5c90d90a857456693f1deab0bd053ffcca96ca7d2f77b2badd|active
```

Signing in end to end through Caddy also works, opaque `HttpOnly` cookie and all
(`cat jar` trimmed to the cookie line — curl writes three comment lines above it):

```
$ curl -sk -c jar -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15557776666"}'
{"devCode":"123456","ok":true}

$ curl -sk -b jar -c jar -X POST https://localhost/v1/auth/otp/verify \
    -d '{"phone":"+15557776666","code":"123456"}'
{"user":{"id":"2c5c16e1-126c-4868-b0b3-899b240eae0d","name":null,"onboardingStep":"intent","status":"pending","role":"user","xp":0,"level":1,"incognito":false,"metroId":null,"profile":null,"photos":[],"intents":[],"preferences":{"user_id":"2c5c16e1-126c-4868-b0b3-899b240eae0d","age_min":18,"age_max":99,"distance_scope":"metro","who_to_meet_open":true,"who_to_meet":null,"created_at":"2026-10-14T21:51:13.787131Z","updated_at":"2026-10-14T21:51:13.787131Z"}}}

$ cat jar
#HttpOnly_localhost	FALSE	/	FALSE	1792021873	gm_session	0f59e7b33667a3f5f7b4ea411f35db512416b7a19f2d74686a28221243d7176c
```

`devCode` is present because `APP_DEBUG=true` in the local block. It must be
absent on the VPS; `docs/superpowers/plans/2026-10-13-docker-deployment.md`
Task 6 is what proves that.

#### Step 8 — the data directory is host-owned, not root-owned

```bash
ls -ld data data/photos
docker compose exec api ls -ld /data
```

```
$ docker compose exec api ls -ld /data
drwxr-xr-x 1 gamematch gamematch        88 Oct 14 21:51 /data
```

uid 10001 on both sides, numerically:

```
$ docker compose exec api id
uid=10001(gamematch) gid=10001(gamematch) groups=10001(gamematch)
$ docker compose exec api ls -ldn /data
drwxr-xr-x    1 10001    10001          100 Oct 14 21:51 /data
```

**`data/photos` does not exist on a fresh stack**, so the plan's
`ls -ld data data/photos` errors on the second path. Immediately after a first
`up` and `seed`:

```
$ ls -ld data data/photos
ls: cannot access 'data/photos': No such file or directory
drwxr-xr-x 1 10001 10001 88 Oct 14 14:51 data
```

On a genuinely fresh install that is **benign**: there are no photos to lose,
and the first backup runs ~35s after `up`, long before anyone can upload
anything. Nothing depends on the directory existing — every `PhotosDir` use site
either creates it (`media.MkdirAll`, `apps/server/internal/httpapi/photos.go:42`,
`apps/server/internal/media/photos.go:104`) or tolerates its absence
(`os.Remove`, `os.RemoveAll` in `httpapi/me.go:226-235`).

The image builds `install -d -o 10001 -g 10001 /data/photos`, but `./data` is a
bind mount, so the host directory shadows the image's `/data` and takes the
`photos` subdirectory with it. That image line buys nothing and is worth knowing
about. The directory is created on the first upload, owned by 10001:

```
$ curl -sk -b jar -X POST https://localhost/v1/me/photos \
    -F "photo=@apps/web/dist/icons/icon-192.png;type=image/png"
{"photo":{"id":"b065faa9-cd1d-4df4-85fc-390f68c06159","url":"/v1/photos/b065faa9-cd1d-4df4-85fc-390f68c06159","state":"ok"}}

$ sqlite3 data/gamematch.db "SELECT id, path FROM photos;"
b065faa9-cd1d-4df4-85fc-390f68c06159|c9454cc0-f98b-4767-8d45-071aa8b4e327/b065faa9-cd1d-4df4-85fc-390f68c06159.jpg

$ ls -ld data data/photos
drwxr-xr-x 1 10001 10001 100 Oct 14 15:01 data
drwxr-x--- 1 10001 10001  72 Oct 14 15:01 data/photos
```

So the ownership property the step protects holds. Note the mode: `0750`, so the
host user cannot `find data/photos` without sudo — that is `media.dirPerm`
(`apps/server/internal/media/photos.go:93`) by design, not a permissions bug.

##### The real exposure: a missing photos directory is skipped in silence

The fresh-install case is a false alarm in the verification step. This is not:

```
$ ls backups/                       # photos/ still present
gamematch-20261014T220057Z.db
gamematch-20261014T220112Z.db
photos-20261014T220112Z.tar.gz

# ... data/photos removed, while the database still holds the photo row ...
$ sqlite3 data/gamematch.db "SELECT count(*) || ' photo rows' FROM photos;"
1 photo rows

$ docker compose run --rm --entrypoint sh backup -c "apk add --no-cache bash sqlite >/dev/null && /scripts/backup.sh /backups"
backup: wrote /backups/gamematch-20261014T220127Z.db
$ echo $?
0

$ ls backups/
gamematch-20261014T220057Z.db
gamematch-20261014T220112Z.db
gamematch-20261014T220127Z.db
```

The third `.db` — `220127Z`, taken with `data/photos` gone — has **no matching
`photos-*.tar.gz`**, and nothing said so.

`scripts/backup.sh:114` guards the archive with `if [ -d "$DATA_DIR/photos" ]`
and otherwise does nothing at all — no warning, exit 0, a valid `.db` written
and published as usual. And nothing else notices:

```
$ docker compose ps backup --format "table {{.Name}}\t{{.Status}}"
NAME                 STATUS
gamematch-backup-1   Up About a minute (healthy)

$ docker compose logs --tail 5 backup
backup-1  | backup: wrote /backups/gamematch-20261014T220057Z.db
```

The healthcheck only looks for a recent `gamematch-*.db`, so it stays `healthy`.
If `data/photos` is ever lost — deleted, restored from the wrong place, left
unmounted by a bind-mount typo — **every subsequent backup is silently
database-only for six hours at a time**, indefinitely, with no signal anywhere.
The database keeps its photo rows, so nothing in the app breaks either; the loss
only surfaces at the point of recovery.

That is the row for Task 7's troubleshooting table:

> **No `photos-*.tar.gz` across several consecutive backups ⇒ the photos
> directory is gone.** Check `ls -ld data/photos`. The script does not warn and
> the backup container stays healthy; the only evidence is the absence of the
> file in `./backups`.

Note also that `./backups` files are owned by `root`, because the `backup`
container runs as root and `./backups` is not remapped. Harmless while only that
container writes there.

One sharp edge on the manual path above: `--entrypoint sh` replaces the
service's `command:`, which is where `apk add` lives. Dropping the `apk add`
gives `env: can't execute 'bash': No such file or directory`, because the script's
shebang is `#!/usr/bin/env bash` and the base image has no bash. Any one-off
backup has to repeat the `apk add`.

#### Step 9 — logs and rotation

```bash
docker compose logs --tail 20 web
docker compose exec web ls -la /var/log/caddy
```

```
web-1  | {"level":"info","ts":1792014601.9798064,"logger":"http","msg":"enabling HTTP/3 listener","addr":":443"}
web-1  | {"level":"info","ts":1792014601.9799697,"logger":"http.log","msg":"server running","name":"srv0","protocols":["h1","h2","h3"]}
web-1  | {"level":"warn","ts":1792014601.9800115,"logger":"http","msg":"HTTP/2 skipped because it requires TLS","network":"tcp","addr":":80"}
web-1  | {"level":"warn","ts":1792014601.9800158,"logger":"http","msg":"HTTP/3 skipped because it requires TLS","network":"tcp","addr":":80"}
web-1  | {"level":"info","ts":1792014601.9800184,"logger":"http.log","msg":"server running","name":"remaining_auto_https_redirects","protocols":["h1","h2","h3"]}
web-1  | {"level":"info","ts":1792014601.9800217,"logger":"http","msg":"enabling automatic TLS certificate management","domains":["localhost"]}
web-1  | {"level":"warn","ts":1792014601.980081,"logger":"pki.ca.local","msg":"installing root certificate (you might be prompted for password)","path":"storage:pki/authorities/local/root.crt"}
web-1  | {"level":"info","ts":1792014601.9803343,"msg":"warning: \"certutil\" is not available, install \"certutil\" with \"apt install libnss3-tools\" or \"yum install nss-tools\" and try again"}
web-1  | {"level":"info","ts":1792014601.9803696,"msg":"define JAVA_HOME environment variable to use the Java trust"}
web-1  | {"level":"info","ts":1792014602.0365372,"logger":"tls","msg":"cleaning storage unit","storage":"FileStorage:/data/caddy"}
web-1  | {"level":"info","ts":1792014602.0367677,"logger":"tls.obtain","msg":"acquiring lock","identifier":"localhost"}
web-1  | {"level":"info","ts":1792014602.2018318,"logger":"tls.obtain","msg":"lock acquired","identifier":"localhost"}
web-1  | {"level":"info","ts":1792014602.2018924,"logger":"tls.obtain","msg":"obtaining certificate","identifier":"localhost"}
web-1  | {"level":"info","ts":1792014602.2019408,"msg":"finished cleaning storage units"}
web-1  | {"level":"info","ts":1792014602.4323099,"logger":"tls.obtain","msg":"certificate obtained successfully","identifier":"localhost","issuer":"local"}
web-1  | {"level":"info","ts":1792014602.4324214,"logger":"tls.obtain","msg":"releasing lock","identifier":"localhost"}
web-1  | {"level":"warn","ts":1792014602.4327657,"msg":"stapling OCSP","identifiers":["localhost"]}
web-1  | {"level":"info","ts":1792014602.4517696,"msg":"certificate installed properly in linux trusts"}
web-1  | {"level":"info","ts":1792014602.451927,"msg":"autosaved config (load with --resume flag)","file":"/config/caddy/autosave.json"}
web-1  | {"level":"info","ts":1792014602.4519334,"msg":"serving initial configuration"}

$ docker compose exec web ls -la /var/log/caddy
total 28
drwxr-xr-x 1 root     root            40 Oct 14 21:50 .
drwxr-xr-x 1 root     root            10 Oct 14 21:50 ..
-rw-------    1 root     root         27394 Oct 14 21:51 gamematch-access.log
```

Started cleanly, no panic, certificate obtained from the internal CA, access log
present. The rotation policy is in the running config, not just the file:

```
$ docker compose exec web caddy adapt --config /etc/caddy/Caddyfile --pretty
	"admin": {
		"disabled": true
	},
	"logging": {
		"logs": {
			"default": {
				"exclude": ["http.log.access.log0"]
			},
			"log0": {
				"writer": {
					"filename": "/var/log/caddy/gamematch-access.log",
					"output": "file",
					"roll_keep": 10,
					"roll_keep_days": 30,
					"roll_size_mb": 100
				},
				"include": ["http.log.access.log0"]
			}
		}
	},
```

`roll_size_mb: 100`, `roll_keep: 10`, `roll_keep_days: 30` (720h). Note
`"admin": {"disabled": true}` — `deploy/Caddyfile:21` is `admin off`, so port
2019 in `docker compose ps` is the base image's `EXPOSE` and nothing is
listening:

```
$ docker compose exec web wget -qO- http://127.0.0.1:2019/config/
wget: can't connect to remote host (127.0.0.1): Connection refused
```

**`caddy reload` does not work on this stack**, and it is the obvious thing to
reach for. It is implemented on top of the admin API:

```
$ docker compose exec web caddy reload --config /etc/caddy/Caddyfile
{"level":"info","ts":1792015261.2054496,"msg":"using config from file","file":"/etc/caddy/Caddyfile"}
{"level":"info","ts":1792015261.2066576,"msg":"adapted config to JSON","adapter":"caddyfile"}
Error: sending configuration to instance: performing request: Post "http://localhost:2019/load": dial tcp [::1]:2019: connect: connection refused
$ echo $?
1
```

(Without `--config` it fails earlier still, with `Error: no config file to load`,
because it looks for `./Caddyfile` in the working directory.)

The two things that do work:

```bash
# read/validate the effective config, offline, without touching the running one
docker compose exec web caddy adapt --config /etc/caddy/Caddyfile --pretty

# apply a Caddyfile change: restart the container
docker compose restart web
```

A `web` restart drops in-flight TLS connections; the service sets
`stop_grace_period: 30s` so Caddy drains first, but a client mid-request at that
moment is cut rather than finished.

---

### A2 — Verified: production boot path

Recorded on a local machine on 2026-10-14, immediately after the section above,
against the same stack. Every local run so far has used `APP_ENV=local`, and
`Config.Validate()` returns `nil` immediately outside production
(`apps/server/internal/config/config.go:119-121`) — so **nothing above this line
exercises it at all.** This section is the only evidence in the repo that the
production guard works through a container.

Same house standard as the section above: real terminal output, no altered
values. Trims are declared inline. The credentials below are **placeholders** —
`ACplaceholder` / `placeholder` / `+15550000000` / `0xplaceholder`. Nothing real
is in `.env` or in this document.

The plan's step 1 **does not pass as written**; the first block is the failure and
the diagnosis, because an operator following the plan hits exactly this.

#### Step 1 — boot in production mode with placeholder credentials

```bash
cp .env .env.local.bak
sed -i 's/^APP_ENV=.*/APP_ENV=production/; s/^APP_DEBUG=.*/APP_DEBUG=false/' .env
printf 'SMS_PROVIDER=twilio\nTWILIO_ACCOUNT_SID=ACplaceholder\nTWILIO_AUTH_TOKEN=placeholder\nTWILIO_FROM=+15550000000\nTURNSTILE_SECRET=0xplaceholder\n' >> .env
docker compose up -d api
sleep 5
docker compose ps api
```

```
 Container gamematch-api-1 Recreate 
 Container gamematch-api-1 Recreated 
 Container gamematch-api-1 Starting 
 Container gamematch-api-1 Started 
NAME              IMAGE                  COMMAND                  SERVICE   CREATED          STATUS                         PORTS
gamematch-api-1   gamematch-api:latest   "/usr/local/bin/game…"   api       12 seconds ago   Restarting (1) 2 seconds ago   
```

```
$ docker compose logs api | tail -20
api-1  | 2026/10/14 22:14:41 config: unsafe production configuration: DEV_OTP (must be unset; it replaces the random OTP with a constant)
api-1  | 2026/10/14 22:14:44 config: unsafe production configuration: DEV_OTP (must be unset; it replaces the random OTP with a constant)
api-1  | 2026/10/14 22:14:47 config: unsafe production configuration: DEV_OTP (must be unset; it replaces the random OTP with a constant)
api-1  | 2026/10/14 22:14:51 config: unsafe production configuration: DEV_OTP (must be unset; it replaces the random OTP with a constant)

$ docker inspect gamematch-api-1 --format "RestartCount={{.RestartCount}} ExitCode={{.State.ExitCode}} Status={{.State.Status}}"
RestartCount=4 ExitCode=1 Status=restarting
```

**Why: the `sed` leaves `DEV_OTP` set.** `.env.example`'s local block ships it
**uncommented** — `DEV_OTP=123456`, line 32 — and the step rewrites only
`APP_ENV` and `APP_DEBUG`. `DevOTP` is `getenv("DEV_OTP", devOTPDefault(env))`
(`config.go:86`), and `devOTPDefault` only supplies the default when the variable
is *absent*, so the literal `123456` from the local block wins. `Validate()` then
refuses it (`config.go:126-128`). One extra line fixes it:

```bash
sed -i '/^DEV_OTP=/d' .env
```

This is the same trap the VPS block warns about twice in its prose ("DEV_OTP is
the one line in this block that stays commented"), reached by a different route.
The file is correct; the step's `sed` is not. **Note the direction of the
failure, though: the refusal is loud, names the variable, and kills the process.
That is the property being verified, and it held here too.**

With `DEV_OTP` removed — a complete production environment, still placeholder
credentials — the step passes:

```
 Container gamematch-api-1 Recreate 
 Container gamematch-api-1 Recreated 
 Container gamematch-api-1 Starting 
 Container gamematch-api-1 Started 
NAME              IMAGE                  COMMAND                  SERVICE   CREATED          STATUS                   PORTS
gamematch-api-1   gamematch-api:latest   "/usr/local/bin/game…"   api       15 seconds ago   Up 5 seconds (healthy)   8000/tcp
```

**Running, not restarting.** The boot line and the environment actually in the
container:

```
$ docker compose logs api | tail -5
api-1  | 2026/10/14 22:15:08 gamematch listening on http://0.0.0.0:8000 (data dir /data, debug=false)
api-1  | 2026/10/14 22:15:10 GET /v1/healthz 200 0s

$ docker compose exec api env | grep -E "^(APP_ENV|APP_DEBUG|DEV_OTP|SMS_PROVIDER|TURNSTILE_SECRET|TWILIO_ACCOUNT_SID)=" | sort
APP_DEBUG=false
APP_ENV=production
SMS_PROVIDER=twilio
TURNSTILE_SECRET=0xplaceholder
TWILIO_ACCOUNT_SID=ACplaceholder
```

`DEV_OTP` is in that grep's pattern and does not appear in the output: the
variable is absent from the container's environment entirely, because the
`sed -i '/^DEV_OTP=/d' .env` removed it from the file `env_file` reads. That is
what `Validate()` requires — not "set to empty", which `getenv` would also treat
as unset but which leaves a confusing line in the file.

#### Step 2 — the API port is still closed in production mode

```bash
curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:8000/v1/healthz
```

```
$ curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:8000/v1/healthz
000

$ docker compose ps --format "{{.Service}} {{.Ports}}"
api 8000/tcp
backup 
web 0.0.0.0:80->80/tcp, [::]:80->80/tcp, 0.0.0.0:443->443/tcp, [::]:443->443/tcp, 443/udp, 2019/tcp

$ docker compose exec web wget -qO- http://api:8000/v1/healthz
{"ok":true}
```

`000`, and still no `->` on the `api` line. Containment is a property of the
compose file, not of the environment, and `APP_ENV=production` does not change
it. The in-network `wget` is there so `000` is not mistaken for a dead API.

#### Step 3 — `APP_DEBUG` is genuinely off

```bash
curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15551230000"}'
```

```
$ curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15551230000"}'
{"ok":true}
```

No `devCode`. **That response alone does not prove `APP_DEBUG` is off.** The
handler has four distinct paths that each answer `{"ok":true}` silently
(`auth.go:120-146`, see "(c2) otp/start fails silently once the rate limit is
hit" under Step 7 above): the IP rate limit, a malformed phone, the
phone rate limit, and public-signup being off with the number un-allowlisted.
Only the fifth path — a code actually issued — omits `devCode` *because* debug is
off, and only it writes an OTP row.

So the discriminator is `otp_codes`:

```
$ sqlite3 data/gamematch.db "SELECT phone, code, code = '123456' AS is_dev_otp FROM otp_codes;"
+15551230000|518585|0
```

One caveat on that row: `PutOTP` upserts on phone conflict
(`store/auth.go:61`), so a row left by an *earlier* request would be refreshed
rather than created — and a row that was already sitting there would look like
proof when nothing had run. Either confirm the table was empty beforehand, or
check the row's `created_at` against the time of the call.

A row exists, so the request ran the whole way through. `is_dev_otp = 0`: the
stored code is random (`randomCode()`, `auth.go:149`), not the constant. Neither
limit was reached and public signup was on, which rules the other four out
directly:

```
$ sqlite3 data/gamematch.db "SELECT bucket, count, window_start FROM rate_limits WHERE bucket LIKE 'otp:start%';"
otp:start:ip:4be83b3115428950|1|1792016120559
otp:start:phone:86d40e3443117dc4|1|1792016120647

$ sqlite3 data/gamematch.db "SELECT key, value FROM feature_flags;"
auth.public_signup|{"v":true}
auth.otp|{"v":true}
games.enabled|{"v":true}
staff.force_pair|{"v":true}
```

Counts of 1 against limits of 5/IP and 3/phone, and `auth.public_signup` is true.

The Twilio send was genuinely attempted and genuinely failed, which is the point
of placeholder credentials — the failure is logged, and the request still
answers 200:

```
$ docker compose logs api | grep -v healthz | tail -8
api-1  | 2026/10/14 22:15:08 gamematch listening on http://0.0.0.0:8000 (data dir /data, debug=false)
api-1  | 2026/10/14 22:15:20 sms send: twilio: status 401: Authentication Error - invalid username
api-1  | 2026/10/14 22:15:20 POST /v1/auth/otp/start 200 309ms
```

`auth.otp` is `{"v":true}` here, so this is a real send, not a skipped one. That
flag is the incident kill switch (`auth.go:156`); it is defaulted by migration
`0008_default_flags.sql:8`, which exists precisely because `seed` writes only
`auth.public_signup` (`seed.go:143`). Expect this line on a VPS with a wrong
`TWILIO_ACCOUNT_SID`.

#### Step 4 — a missing variable still fails the boot

This is the Review Focus item: *if the entrypoint swallowed the exit code, a VPS
would deploy a silently non-functional service.*

```bash
sed -i '/^TURNSTILE_SECRET=/d' .env
docker compose up -d --force-recreate api
sleep 5
docker compose logs api | tail -20
docker compose ps api
```

```
$ docker compose logs api | tail -20
api-1  | 2026/10/14 22:15:34 config: unsafe production configuration: TURNSTILE_SECRET (required in production)
api-1  | 2026/10/14 22:15:37 config: unsafe production configuration: TURNSTILE_SECRET (required in production)
api-1  | 2026/10/14 22:15:39 config: unsafe production configuration: TURNSTILE_SECRET (required in production)
api-1  | 2026/10/14 22:15:42 config: unsafe production configuration: TURNSTILE_SECRET (required in production)
```

The exit code survives the container boundary:

```
$ docker inspect gamematch-api-1 --format "Status={{.State.Status}} ExitCode={{.State.ExitCode}} RestartCount={{.RestartCount}} Error={{.State.Error}}"
Status=restarting ExitCode=1 RestartCount=8 Error=
```

**`docker compose ps api` does not reliably show this, and that is the one sharp
edge in this section.** `restart: unless-stopped` on the `api` service
(`docker-compose.yml:40`) means the container never comes to rest in `Exited
(1)` — it crash-loops, so a single `ps` sample lands at an arbitrary point in the
cycle and alternates between two readings, both of which look benign:

```
$ for i in 1 2 3 4 5 6; do docker compose ps api --format '{{.Status}}'; sleep 2; done
Up 1 second (health: starting)
Up 1 second (health: starting)
Restarting (1) 3 seconds ago
Restarting (1) 2 seconds ago
Restarting (1) 4 seconds ago
Restarting (1) 6 seconds ago
```

An operator who runs `docker compose ps` once during a deploy and sees
`Up 1 second (health: starting)` concludes the service is starting normally. It
is crashing roughly three times a second. **Read the log — that is the check
that cannot be misread.**

Do **not** reach for `.State.ExitCode` instead. It resets to `0` every time the
container restarts, so during a crash loop it reads `0` once per cycle, in the
brief window between the restart and `Validate()` killing the process again.
Measured here: 7 of the first 50 samples were `0`. That is a worse misread than
`ps`, because `0` looks like a definitive clean result rather than an ambiguous
status — and on a healthy boot `ExitCode` is `0` too, so the two cases are
indistinguishable.

`RestartCount` is monotonic and strictly non-decreasing, so it has neither
hazard:

```bash
docker inspect --format '{{.RestartCount}}' gamematch-api-1
```

Any non-zero value after a deploy means the API is not staying up.

What the front end does while this is happening:

```
$ curl -sk -o /dev/null -w 'static /      -> %{http_code}\n' https://localhost/
static /      -> 200
$ curl -sk -o /dev/null -w 'proxied /v1  -> %{http_code}\n' https://localhost/v1/healthz
proxied /v1  -> 502
```

The PWA still loads and the API is 502 — so "the site loads" is not evidence the
API booted. And the proxy error is **not** `connection refused`:

```
web-1  | {"level":"error","ts":1792016164.8125532,"logger":"http.log.error.log0","msg":"dial tcp: lookup api on 127.0.0.11:53: server misbehaving","request":{...,"uri":"/v1/healthz",...},"duration":0.005412973,"status":502,"err_id":"sw5hbwhje","err_trace":"reverseproxy.statusError (reverseproxy.go:1390)"}
```

It is Docker's embedded DNS failing to resolve the service name, because the
`api` container is being torn down and recreated constantly. Anyone reading this
as "Caddy cannot find the api service" and editing `GAMEMATCH_API` will fix
nothing. (The raw log line is a single line whose `request` object holds the full
headers and TLS detail; it is abbreviated to `...` here for width. No value in the
line has been altered.)

#### Step 5 — restore local config

```bash
mv .env.local.bak .env
docker compose up -d --force-recreate api
docker compose ps
```

```
NAME                 IMAGE                  COMMAND                  SERVICE   CREATED              STATUS                    PORTS
gamematch-api-1      gamematch-api:latest   "/usr/local/bin/game…"   api       14 seconds ago   Up 10 seconds (healthy)   8000/tcp
gamematch-backup-1   alpine:3.24            "sh -c 'apk add --no…"   backup    2 minutes ago    Up 2 minutes (healthy)    
gamematch-web-1      gamematch-web:latest   "caddy run --config …"   web       2 minutes ago    Up About a minute         
```

```
$ docker compose logs api | tail -3
api-1  | 2026/10/14 22:16:11 gamematch listening on http://0.0.0.0:8000 (data dir /data, debug=true)
api-1  | 2026/10/14 22:16:13 GET /v1/healthz 200 0s

$ curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15551237777"}'
{"devCode":"123456","ok":true}
```

`debug=true` in the boot line, and `devCode` is back. That last line is the
control for step 3: **the same request returns `devCode` under
`APP_ENV=local`/`APP_DEBUG=true` and omits it under production**, on the same
stack, minutes apart.

#### Summary for the runbook

Two things an operator needs to know that the plan does not say:

1. **`.env` must not carry `DEV_OTP` into production, even in the local block.**
   The whole point of switching hosts is editing values in place, and `DEV_OTP`
   survives that edit. `Validate()` catches it, loudly — but the deploy fails
   first time with `DEV_OTP (must be unset...)`, which reads like a config bug
   rather than the documented "edit the local block into the VPS block"
   instruction. Delete the line, do not just change `APP_ENV`.
2. **A production misconfiguration shows as a crash loop, not as `Exited`.**
   Because of `restart: unless-stopped`, `docker compose ps` samples the loop at
   an arbitrary point and usually reads `Up 1 second (health: starting)`. Read
   the log, and confirm with `docker inspect --format '{{.RestartCount}}'`.
   *Not* `.State.ExitCode`, which resets to `0` on every restart and reads `0`
   on a healthy boot too. The symptom an operator will actually see on the site
   is a PWA that loads and an API that 502s.

Both are now written up in [Deploying to a VPS](#deploying-to-a-vps) and in
[Troubleshooting](#troubleshooting).
---

## Appendix B — verification evidence (continued)

Everything below was recorded on 2026-10-14 against the same stack, after the
work in [Appendix A](#appendix-a--verification-evidence). Same standard: real
terminal output, no altered values, trims declared inline.

### B1 — the `.env` VPS-block transformation, end to end

The procedure in [step 3](#3-fill-in-the-production-variables) is two `sed`
commands. They are worth executing rather than publishing untested, because the
exact failure they exist to prevent — a crash loop on `DEV_OTP` — was hit
during this plan's Task 6.

Run against a copy of `.env.example`, placeholder credentials appended:

```
$ cp .env.example /tmp/gm.env
$ sed -i 's|^\([A-Z][A-Z_0-9]*=\)|#\1|' /tmp/gm.env
$ sed -i '/^# --- VPS:/,/^# --- optional:/{/^#DEV_OTP=/!{/^#[A-Z][A-Z_0-9]*=/s/^#//}}' /tmp/gm.env
$ sed -i 's|^GAMEMATCH_SITE=.*|GAMEMATCH_SITE=localhost|' /tmp/gm.env
$ printf 'TWILIO_ACCOUNT_SID=ACplaceholder\nTWILIO_AUTH_TOKEN=placeholder\nTWILIO_FROM=+15550000000\nTURNSTILE_SECRET=0xplaceholder\n' >> /tmp/gm.env

$ grep -E '^[A-Z]|^#DEV_OTP|^#VAPID' /tmp/gm.env
APP_ENV=production
GAMEMATCH_SITE=localhost
HTTP_PORT=80
HTTPS_PORT=443
APP_DEBUG=false
#DEV_OTP=
SMS_PROVIDER=twilio
TWILIO_ACCOUNT_SID=
TWILIO_AUTH_TOKEN=
TWILIO_FROM=
TURNSTILE_SECRET=
HOUSE_USER_ID=00000000-0000-4000-8000-000000000001
#VAPID_PUBLIC_KEY=
#VAPID_PRIVATE_KEY=
#VAPID_SUBJECT=mailto:ops@example.com
TWILIO_ACCOUNT_SID=ACplaceholder
TWILIO_AUTH_TOKEN=placeholder
TWILIO_FROM=+15550000000
TURNSTILE_SECRET=0xplaceholder
```

Four things to read off that:

- **The local block is gone** — every one of its lines starts with `#`. It does
  not appear in this listing at all.
- **`#DEV_OTP=` stays commented**, exactly as `.env.example` line 8 requires.
- **The optional web-push block stays commented.** It sits after the
  `# --- optional:` marker, outside the range the second `sed` touches.
- **Appending is safe.** The three `TWILIO_*` keys appear twice — once empty from
  the uncommented block, once with a value from the `printf` — and the container
  receives the **later** one. That is the "fill the values in" shortcut, verified
  rather than assumed:

  ```
  $ docker compose exec api env | grep -E "^(APP_ENV|APP_DEBUG|DEV_OTP|SMS_PROVIDER|TWILIO_[A-Z_]+|TURNSTILE_SECRET|GAMEMATCH_SITE)=" | sort
  APP_DEBUG=false
  APP_ENV=production
  GAMEMATCH_SITE=localhost
  SMS_PROVIDER=twilio
  TURNSTILE_SECRET=0xplaceholder
  TWILIO_ACCOUNT_SID=ACplaceholder
  TWILIO_AUTH_TOKEN=placeholder
  TWILIO_FROM=+15550000000
  ```

`DEV_OTP` is in that grep's pattern and does not appear: it is absent from the
container's environment entirely, which is what `Validate()` requires. "Set to
empty" would also be read as unset, but it leaves a confusing line in the file.

The whole documented path boots, with no crash loop:

```
$ docker compose up -d --force-recreate api web
 Container gamematch-api-1 Recreate
 Container gamematch-api-1 Recreated
 Container gamematch-web-1 Recreate
 Container gamematch-web-1 Recreated
 Container gamematch-api-1 Starting
 Container gamematch-api-1 Started
 Container gamematch-api-1 Waiting
 Container gamematch-api-1 Healthy
 Container gamematch-web-1 Starting
 Container gamematch-web-1 Started

$ docker compose ps
NAME                 IMAGE                  COMMAND                  SERVICE   CREATED          STATUS                    PORTS
gamematch-api-1      gamematch-api:latest   "/usr/local/bin/game…"   api       24 seconds ago   Up 14 seconds (healthy)   8000/tcp
gamematch-backup-1   alpine:3.24            "sh -c 'apk add --no…"   backup    6 minutes ago    Up 6 minutes (healthy)
gamematch-web-1      gamematch-web:latest   "caddy run --config …"   web       19 seconds ago   Up 10 seconds             0.0.0.0:80->80/tcp, [::]:80->80/tcp, 0.0.0.0:443->443/tcp, [::]:443->443/tcp, 443/udp, 2019/tcp

$ docker inspect --format '{{.RestartCount}}' gamematch-api-1
0

$ docker compose logs api | tail -3
api-1  | 2026/10/14 22:34:28 gamematch listening on http://0.0.0.0:8000 (data dir /data, debug=false)
api-1  | 2026/10/14 22:34:30 GET /v1/healthz 200 0s
```

`GAMEMATCH_SITE` is `localhost` here, not a real domain: the transformation is
what is under test, and a real domain would send this container to the public
ACME service. On the VPS it is your domain — see [B2](#b2--what-a-failed-certificate-looks-like)
for what happens when it is not resolvable.

**The `sed` delimiter.** GNU sed does not do what `\#` looks like it does. With
`/` as the delimiter, `\#` does not mean "a literal `#` in the replacement":

```
$ printf 'A=1\nB=2\n' | sed 's/^\([A-Z][A-Z_0-9]*=\)/\/#\1/'
/#A=1
/#B=2
$ printf 'A=1\nB=2\n' | sed 's|^\([A-Z][A-Z_0-9]*=\)|#\1|'
#A=1
#B=2
```

The first form leaves `/#APP_ENV=local` on every line of the local block, which
Compose does not read as a comment. The `|` delimiter does not have the problem.

### B2 — what a failed certificate looks like

The plan requires DNS to point at the host before the first start. The *symptom*
of getting that wrong is worth knowing, because it is not an obvious one.

Produced deliberately by pointing `GAMEMATCH_SITE` at a name that cannot be
issued for. **`.invalid` is reserved by RFC 2606 and is never registered**, so
no real certificate was issued and no real domain was touched — but Caddy does
still register an anonymous ACME account with Let's Encrypt as part of trying,
which is the cost of this test:

```
$ sed -i 's|^GAMEMATCH_SITE=.*|GAMEMATCH_SITE=gamematch-does-not-exist.invalid|' .env
$ docker compose up -d --force-recreate web
...
web-1  | {"level":"info","ts":1792017148.891791,"logger":"tls.obtain","msg":"acquiring lock","identifier":"gamematch-does-not-exist.invalid"}
web-1  | {"level":"info","ts":1792017148.9162717,"logger":"tls.obtain","msg":"lock acquired","identifier":"gamematch-does-not-exist.invalid"}
web-1  | {"level":"info","ts":1792017148.9164073,"logger":"tls.obtain","msg":"obtaining certificate","identifier":"gamematch-does-not-exist.invalid"}
web-1  | {"level":"info","ts":1792017149.3780818,"logger":"http","msg":"using ACME account","account_id":"https://acme-v02.api.letsencrypt.org/acme/acct/3864904546","account_contact":[]}
web-1  | {"level":"error","ts":1792017149.4332113,"logger":"tls.obtain","msg":"could not get certificate from issuer","identifier":"gamematch-does-not-exist.invalid","issuer":"acme-v02.api.letsencrypt.org-directory","error":"HTTP 400 urn:ietf:params:acme:error:rejectedIdentifier - Invalid identifiers requested :: Cannot issue for \"gamematch-does-not-exist.invalid\": Domain name does not end with a valid public suffix (TLD)"}
web-1  | {"level":"error","ts":1792017149.4332533,"logger":"tls.obtain","msg":"will retry","error":"[gamematch-does-not-exist.invalid] Obtain: [...] creating new order: attempt 1: https://acme-v02.api.letsencrypt.org/acme/new-order: HTTP 400 ... (ca=https://acme-v02.api.letsencrypt.org/directory)","attempt":1,"retrying_in":60,"elapsed":0.516964286,"max_duration":2592000}
```

The `will retry` line with `retrying_in` and `max_duration: 2592000` (30 days) is
the shape to recognise. Caddy keeps trying, so **the container stays `Up` the
whole time** and `docker compose ps` looks healthy. A domain that resolves but
has no A record pointing here fails at the HTTP-01 challenge instead, with a
different message; the log signature to look for either way is
`docker compose logs web | grep tls.obtain`.

What the client sees is the important part, and it is not a partial outage:

```
$ curl -sk -o /dev/null -w 'static /      -> %{http_code}\n' --max-time 5 https://localhost/
static /      -> 000
$ curl -sk -o /dev/null -w 'proxied /v1  -> %{http_code}\n' --max-time 5 https://localhost/v1/healthz
proxied /v1  -> 000
$ curl -sS --max-time 5 https://localhost/
curl: (35) TLS connect error: error:0A000438:SSL routines::tlsv1 alert internal error
```

Plain HTTP still redirects, to an HTTPS that cannot complete — so "the redirect
works" proves nothing:

```
$ curl -s -o /dev/null -D - --max-time 5 http://localhost/
HTTP/1.1 308 Permanent Redirect
Connection: close
Location: https://localhost/
```

Contrast with the API crash loop in Appendix A.2 step 4, where `/` is `200` and
only `/v1` is `502`. **A missing certificate takes down the whole origin, static
assets included.** Probing two URLs tells the two apart.

Reverting `GAMEMATCH_SITE` to a name Caddy can serve and restarting `web`
recovers with no other change — the ACME account and certificate live in the
`caddy-data` volume, so a restart is enough.

### B3 — the restore drill

Run end to end against a live stack. This is the step that decides whether the
backup is worth having, so the failures are recorded as well as the result.
#### Setup — rows worth losing

```
$ docker compose run --rm api seed
2026/10/14 22:48:33 seeded /data/gamematch.db

$ curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15551239999"}'
{"devCode":"123456","ok":true}

$ CODE="$(sqlite3 data/gamematch.db "SELECT code FROM otp_codes WHERE phone = '+15551239999';")"
$ curl -sk -X POST https://localhost/v1/auth/otp/verify \
    -d "{\"phone\":\"+15551239999\",\"code\":\"$CODE\"}" -o /dev/null -w '%{http_code}\n'
200

$ curl -sk -b jar -X POST https://localhost/v1/me/photos \
    -F "photo=@apps/web/dist/icons/icon-192.png;type=image/png"
{"photo":{"id":"6e961a3e-4665-450e-864d-18c97755b2fe","url":"/v1/photos/6e961a3e-4665-450e-864d-18c97755b2fe","state":"ok"}}

$ ls -ldn data data/photos
drwxr-xr-x 1 10001 10001 100 Oct 14 15:48 data
drwxr-x--- 1 10001 10001  72 Oct 14 15:48 data/photos

$ sqlite3 data/gamematch.db 'SELECT count(*) FROM users;'
5
```

`devCode` is present here because `APP_ENV=local` / `APP_DEBUG=true`. **That is a
convenience, not a requirement.** `otp_codes.code` is stored in plaintext —
`PutOTP` writes the code verbatim into the column
(`apps/server/internal/store/auth.go:59-63`) — so a drill can read a real code off
a production-mode database and the only difference is that it is a random code
rather than the constant `123456`:

```
$ docker compose exec api env | grep -E '^(APP_ENV|APP_DEBUG)=' | sort
APP_DEBUG=false
APP_ENV=production

$ curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15559990000"}'
{"ok":true}

$ sqlite3 data/gamematch.db "SELECT phone, code, code = '123456' AS is_dev_otp FROM otp_codes WHERE phone = '+15559990000';"
+15559990000|423454|0
```

`is_dev_otp = 0` — a random code, read straight out of a **production** database,
and **no `devCode` in the response** because `APP_DEBUG=false`. This drill was
recorded in local mode only because that is what this host's `.env` held; nothing
about it requires `APP_ENV=local`.

**Do not run a production host in `APP_ENV=local` / `APP_DEBUG=true` to make a
drill easier.** That makes every one-time code the constant `123456` and returns
it in the response body, so anyone who can reach the site can sign in as any
phone number — the exact hole `Validate()` exists to close, and the subject of the
first `DEV_OTP` row in [Troubleshooting](#troubleshooting).

(The Twilio send in production mode genuinely fails with
`sms send: twilio: status 401: Authentication Error - invalid username` against
placeholder credentials. The request still answers `200`, which is the silent
credential-miss signature from [Troubleshooting](#troubleshooting).)

#### The scheduled backup lands

The `backup` service runs once about 35 seconds after start (`apk add`, then
`sleep 30`, then the script). No manual invocation:

```
$ docker compose ps backup
NAME                 IMAGE         COMMAND                  SERVICE   CREATED              STATUS                        PORTS
gamematch-backup-1   alpine:3.24   "sh -c 'apk add --no…"   backup    About a minute ago   Up About a minute (healthy)   

$ docker compose logs --tail 5 backup
backup-1  | backup: wrote /backups/gamematch-20261014T224853Z.db

$ ls -la backups/
total 516
drwxr-xr-x 1 duke duke      118 Oct 14 15:48 .
drwxr-xr-x 1 duke duke      438 Oct 14 15:47 ..
-rw-r--r-- 1 root root 516096 Oct 14 22:48 gamematch-20261014T224853Z.db
-rw-r--r-- 1 root root   8991 Oct 14 22:48 photos-20261014T224853Z.tar.gz
```

Both artefacts, from the loop, unprompted. `photos-*.tar.gz` is present only
because the upload above created `data/photos` — see [Backups](#backups) for what
its absence means.

The on-demand path, also verified:

```
$ docker compose exec backup /scripts/backup.sh /backups
backup: wrote /backups/gamematch-20261014T225343Z.db
rc=0
```

And the form that does not work, for the record:

```
$ docker compose run --rm --entrypoint /scripts/backup.sh backup /backups
 Container gamematch-api-1 Running
 Container gamematch-backup-run-0e0300581ef2 Creating
 Container gamematch-backup-run-0e0300581ef2 Created
env: can't execute 'bash': No such file or directory

rc=127
```

Overriding the entry point replaces the service's `command:`, which is where
`apk add --no-cache bash sqlite` lives. Stock alpine has **no bash** *and* **no
`sqlite3`** — `command -v sqlite3` in a bare `alpine:3.24` prints nothing — which
is why the restore block below has to install one for itself.

#### Write more rows after the snapshot, so the restore has something to lose

One more user and one more photo, both **after** `224853Z` was taken:

```
$ curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15552228888"}'
{"devCode":"123456","ok":true}
$ curl -sk -X POST https://localhost/v1/auth/otp/verify \
    -d "{\"phone\":\"+15552228888\",\"code\":\"$CODE\"}" -o /dev/null -w 'verify=%{http_code}\n'
verify=200
$ curl -sk -b jar -X POST https://localhost/v1/me/photos \
    -F "photo=@apps/web/dist/icons/icon-192.png;type=image/png"
{"photo":{"id":"c9a7334b-d305-43d0-a3e1-f2aa4a15e9a8","url":"/v1/photos/c9a7334b-d305-43d0-a3e1-f2aa4a15e9a8","state":"ok"}}

$ sqlite3 data/gamematch.db 'SELECT count(*) FROM users;'
6
$ sqlite3 data/gamematch.db 'SELECT id, path FROM photos;'
6e961a3e-4665-450e-864d-18c97755b2fe|37aa1c41-127c-4e00-9493-a31f5e18b33b/6e961a3e-4665-450e-864d-18c97755b2fe.jpg
c9a7334b-d305-43d0-a3e1-f2aa4a15e9a8|8c7bc12b-df78-4692-a067-bc4a91a3f1e6/c9a7334b-d305-43d0-a3e1-f2aa4a15e9a8.jpg
```

Six live users against five in the snapshot; two live photos against one. **The
drill restores `224853Z`.** Restoring the newest snapshot instead would return a
count equal to the live one and prove nothing — the whole test is that data
written *after* the snapshot is gone.

#### The plan's commands fail for a non-root deploy user

The plan's step 4 is `mv`, `rm` and `cp` inside `./data`. On a VPS where the
deploy user owns the checkout, `./data` is `drwxr-xr-x` owned by `10001`, so
only UID 10001 can create or rename an entry in it, and those three commands are
refused:

```
$ docker compose stop api
 Container gamematch-api-1 Stopping
 Container gamematch-api-1 Stopped

$ sqlite3 data/gamematch.db 'SELECT count(*) FROM users;'
Parse error in 2nd command line argument: attempt to write a readonly database (8)

$ mv data/gamematch.db data/gamematch.db.pre-restore
mv: cannot move 'data/gamematch.db' to 'data/gamematch.db.pre-restore': Permission denied

$ cp "$STAMP" data/gamematch.db
cp: cannot create regular file 'data/gamematch.db': Permission denied
```

The `sqlite3` failure is the more interesting one and it is **not** about
permissions on the file. `sqlite3` opens read-write by default, and a WAL
database needs its `-shm` sidecar; with the API stopped there is no `-shm`, so
SQLite tries to create one in a directory the host user does not own. The
sidecars are gone precisely *because* the API was stopped cleanly — `./data` holds
the database and the photo directory and nothing else:

```
$ ls -ln data
-rw-r--r-- 1 10001 10001 516096 Oct 14 15:49 gamematch.db
drwxr-x--- 1 10001 10001   144 Oct 14 15:49 photos

$ docker compose start api >/dev/null; sleep 4
$ ls -ln data
-rw-r--r-- 1 10001 10001 516096 Oct 14 15:49 gamematch.db
-rw-r--r-- 1 10001 10001 32768 Oct 14 15:52 gamematch.db-shm
-rw-r--r-- 1 10001 10001     0 Oct 14 15:52 gamematch.db-wal
drwxr-x--- 1 10001 10001   144 Oct 14 15:49 photos

$ sqlite3 data/gamematch.db 'SELECT count(*) FROM users;'
6
rc=0
```

The API recreates both sidecars on open and the same host-side query works. So:
**verify either as root, or after the API is back up** — or from inside the
`backup` container, which has `sqlite3` and the same bind mount and answers either
way:

```
$ docker compose stop api >/dev/null
$ docker compose exec backup sqlite3 /data/gamematch.db 'PRAGMA journal_mode;'
wal
rc=0
$ sqlite3 data/gamematch.db 'PRAGMA journal_mode;'
rc=1
Parse error in 2nd command line argument: attempt to write a readonly database (8)
$ docker compose start api >/dev/null
```

The same mechanism is why the `backup` service's `./data` mount cannot be `:ro`
(`docker-compose.yml`).

#### The restore, as root

Same operations, done with the privileges they need. On a VPS, `sudo` instead of
the container — `restore.md` publishes the `sudo` form.

**`apk add --no-cache sqlite` and `set -e` are load-bearing here.** Stock alpine
has no `sqlite3`, so without the first line the `sqlite3` call below fails with
`sqlite3: not found` and rc 127 — and `sh -c` has no `set -e`, so the script
carries straight on and publishes a restore transcript anyway. Without the second,
nothing at all stops a failing step from being recorded as a succeeding one. This
is the exact shape of bug the drill exists to catch, and it is why the block below
shows its `apk` output in full:

```
$ docker run --rm -v "$PWD/data:/d" -v "$PWD/backups:/b:ro" alpine:3.24 sh -c "
    set -e
    apk add --no-cache sqlite
    echo -n 'live users before restore: '; sqlite3 /d/gamematch.db 'SELECT count(*) FROM users;'
    mv /d/gamematch.db /d/gamematch.db.pre-restore
    rm -f /d/gamematch.db-wal /d/gamematch.db-shm
    mv /d/photos /d/photos.pre-restore
    cp /b/gamematch-20261014T224853Z.db /d/gamematch.db
    tar xzf /b/photos-20261014T224853Z.tar.gz -C /d
    chown -R 10001:10001 /d
    ls -ln /d
  "
(1/4) Installing ncurses-terminfo-base (6.6_p20260516-r0)
(2/4) Installing libncursesw (6.6_p20260516-r0)
(3/4) Installing readline (8.3.3-r1)
(4/4) Installing sqlite (3.53.4-r0)
Executing busybox-1.37.0-r31.trigger
OK: 10.7 MiB in 20 packages
live users before restore: 6
total 1008
-rw-r--r-- 1 10001    10001       516096 Oct 14 22:49 gamematch.db
-rw-r--r-- 1 10001    10001       516096 Oct 14 22:49 gamematch.db.pre-restore
drwxr-x--- 1 10001    10001           72 Oct 14 22:49 photos
drwxr-x--- 1 10001    10001          144 Oct 14 22:49 photos.pre-restore
```

**6 live, 5 in the snapshot.** That difference is the loss the drill is built
around.

`mv /d/photos /d/photos.pre-restore` is there for the same reason the database is
moved aside: extracting the archive over a live `data/photos` leaves every
photo directory uploaded **after** the snapshot sitting there, orphaned from a
database that no longer references them. Move it, restore over it, then delete
both at the end (below).

The photo archive itself:

```
$ tar tzf backups/photos-20261014T224853Z.tar.gz
photos/
photos/37aa1c41-127c-4e00-9493-a31f5e18b33b/
photos/37aa1c41-127c-4e00-9493-a31f5e18b33b/6e961a3e-4665-450e-864d-18c97755b2fe.jpg
photos/37aa1c41-127c-4e00-9493-a31f5e18b33b/6e961a3e-4665-450e-864d-18c97755b2fe_t.jpg
```

restored with UID 10001 and mode 0640:

```
$ docker run --rm -v "$PWD/data:/d:ro" alpine:3.24 find /d/photos | sort
/d/photos
/d/photos/37aa1c41-127c-4e00-9493-a31f5e18b33b
/d/photos/37aa1c41-127c-4e00-9493-a31f5e18b33b/6e961a3e-4665-450e-864d-18c97755b2fe.jpg
/d/photos/37aa1c41-127c-4e00-9493-a31f5e18b33b/6e961a3e-4665-450e-864d-18c97755b2fe_t.jpg
```

Exactly the snapshot's photo, and **not** the one uploaded after it. That listing
also shows why it has to be read from a container: `data/photos` is mode `0750`,
so the host user gets `ls: cannot open directory 'data/photos': Permission denied`
and a restore check that tries looks like a failure. `media.dirPerm`
(`apps/server/internal/media/photos.go:93`) is deliberate.

#### The verification

```
$ docker compose start api
 Container gamematch-api-1 Starting
 Container gamematch-api-1 Started

$ docker compose ps api
NAME              IMAGE                  COMMAND                  SERVICE   CREATED           STATUS                   PORTS
gamematch-api-1   gamematch-api:latest   "/usr/local/bin/game…"   api       About a minute    Up 4 seconds (healthy)   8000/tcp

$ curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/v1/healthz
200

$ sqlite3 data/gamematch.db 'SELECT count(*) FROM users;' > /tmp/opencode/after.txt
$ cat /tmp/opencode/after.txt
5
$ diff /tmp/opencode/before.txt /tmp/opencode/after.txt && echo "restore verified"
restore verified
```

**`restore verified`.** `before` and `after` are both `5`, matching the 5 users in
the snapshot, against 6 in the live database before the restore.

The counts alone prove the file was copied. These prove it was the *right* file,
and that the restore actually rolled something back:

```
$ sqlite3 data/gamematch.db 'PRAGMA integrity_check;'
ok
$ sqlite3 data/gamematch.db 'SELECT count(*) FROM sqlite_master;'
106
$ sqlite3 data/gamematch.db "SELECT count(*) FROM users u JOIN user_private p ON p.user_id=u.id WHERE p.phone_e164 = '+15552228888';"
0
$ sqlite3 data/gamematch.db 'SELECT id, path FROM photos;'
6e961a3e-4665-450e-864d-18c97755b2fe|37aa1c41-127c-4e00-9493-a31f5e18b33b/6e961a3e-4665-450e-864d-18c97755b2fe.jpg
$ sqlite3 data/gamematch.db 'PRAGMA journal_mode;'
wal
$ sqlite3 data/gamematch.db 'PRAGMA user_version;'
9
$ docker inspect --format 'RestartCount={{.RestartCount}} Status={{.State.Status}}' gamematch-api-1
RestartCount=0 Status=running
```

The user created **after** the snapshot is **absent**. The photo uploaded after
it is absent from both the database and the directory. The restored database is
WAL-mode at `user_version 9` — the same schema level the live one was.

The `rate_limits` table rolled back too, which is the cleanest cross-check,
because it is a number whose pre-restore value is known:

```
$ sqlite3 data/gamematch.db "SELECT bucket, count FROM rate_limits WHERE bucket LIKE 'otp:start%';"
otp:start:ip:245c0ffc0f6a0215|1
otp:start:phone:834fa3a38e93d341|1
```

A count of `1` for the per-IP bucket, when three sign-ins had been made during
the drill. The snapshot was taken after the first one. **A restore that had
silently not happened would show `3`.**

#### Clear the aside files

```
$ docker run --rm -v "$PWD/data:/d" alpine:3.24 sh -c "
    rm -rf /d/gamematch.db.pre-restore /d/photos.pre-restore
    ls -ln /d
  "
total 536
-rw-r--r-- 1 10001 10001 516096 Oct 14 22:49 gamematch.db
-rw-r--r-- 1 10001 10001 32768 Oct 14 22:49 gamematch.db-shm
-rw-r--r-- 1 10001 10001     0 Oct 14 22:49 gamematch.db-wal
drwxr-x--- 1 10001 10001    72 Oct 14 22:49 photos
```

`./data` is back to exactly what a steady-state stack holds. Until this step
`./data` also held a **complete stale copy of the pre-restore database** and a
stale photo tree, neither of which anything prunes — `gamematch.db.pre-restore`
is a full SQLite database the size of the live one, and it sits inside the
directory the backup service archives from. Delete both once the restore is
verified; keep them only for as long as you might still need to roll back.

#### One artefact worth knowing about

Reading a snapshot with a plain `sqlite3 <file>` leaves `-shm` and `-wal` beside
it — even with `mode=ro`, because a WAL database's index lives in the shared
memory file:

```
$ sqlite3 "backups/gamematch-20261014T224853Z.db" 'SELECT count(*) FROM users;'
5
$ ls -ln backups/
-rw-r--r-- 1    0    0 516096 Oct 14 22:48 gamematch-20261014T224853Z.db
-rw-r--r-- 1 1000 1000 32768 Oct 14 22:50 gamematch-20261014T224853Z.db-shm
-rw-r--r-- 1    0    0     0 Oct 14 22:49 gamematch-20261014T224853Z.db-wal
```

That is 32 KiB per snapshot — roughly 46 MB a year at four runs a day — and
neither name matched `scripts/backup.sh`'s prune, so they used to accumulate for
the life of a deployment. `backup.sh` now sweeps them, and **the rule is
"orphaned", not "old"**: a sidecar is removed only when the `.db` it belongs to
is gone. A sidecar whose snapshot still exists survives no matter how old, because
that snapshot may be being read right now.

Verified against the live stack — a real orphan (no `.db`) planted alongside a live
snapshot's sidecars, then one ordinary scheduled run:

```
$ touch backups/gamematch-20990101T000000Z.db-shm backups/gamematch-20990101T000000Z.db-wal
$ docker compose exec backup /scripts/backup.sh /backups
backup: wrote /backups/gamematch-20261014T225132Z.db
$ ls -ln backups/
-rw-r--r-- 1    0    0 516096 Oct 14 22:48 gamematch-20261014T224853Z.db
-rw-r--r-- 1 1000 1000 32768 Oct 14 22:50 gamematch-20261014T224853Z.db-shm
-rw-r--r-- 1    0    0     0 Oct 14 22:49 gamematch-20261014T224853Z.db-wal
-rw-r--r-- 1    0    0 516096 Oct 14 22:51 gamematch-20261014T225132Z.db
-rw-r--r-- 1    0    0   8991 Oct 14 22:48 photos-20261014T224853Z.tar.gz
-rw-r--r-- 1    0    0   8993 Oct 14 22:51 photos-20261014T225132Z.tar.gz
```

The `20990101T000000Z` sidecars are gone; `224853Z`'s two sidecars survived,
because `224853Z.db` is right there beside them.

The full matrix, in a throwaway directory and run twice — once under busybox
`ash` in `alpine:3.24`, once under GNU bash/coreutils on the host, with identical
results both times:

| Fixture | Expected | Result |
| --- | --- | --- |
| `gamematch-LIVE.db` + `-shm` + `-wal` | all three survive | survived |
| `gamematch-GONE.db-shm` + `-wal`, no `.db` | sidecars removed | removed |
| `gamematch-OLD.db` + sidecars, all dated 30 days ago | `.db` pruned by age, sidecars swept with it in the same run | all three removed |
| `.tmp.INFLIGHT/gamematch-….db{,-shm,-wal}` | untouched — a concurrent run's scratch | untouched |

```
$ docker run --rm -v …/scripts:/scripts:ro -v …/b:/b alpine:3.24 sh /run.sh
backup: wrote /b/gamematch-20261014T225121Z.db
rc=0
$ find . | sort
./gamematch-20261014T225121Z.db
./gamematch-LIVE.db
./gamematch-LIVE.db-shm
./gamematch-LIVE.db-wal
./.tmp.INFLIGHT/gamematch-20261014T999999Z.db
./.tmp.INFLIGHT/gamematch-20261014T999999Z.db-shm
./.tmp.INFLIGHT/gamematch-20261014T999999Z.db-wal

$ DATA_DIR=…/src bash scripts/backup.sh …/b2
backup: wrote …/b2/gamematch-20261014T225125Z.db
rc=0
```

(both listings identical — the LIVE pair kept, the orphans and the 30-day-old set
gone, the in-flight scratch untouched)

The sidecars still cannot corrupt a restore: the procedure copies the `.db` and
not the sidecars, which is correct. Do not start copying `*.db*`.
