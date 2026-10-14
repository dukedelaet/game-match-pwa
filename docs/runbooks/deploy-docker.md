# Docker deployment

> **Status: partial.** This file currently holds verification evidence recorded by
> the deployment plan — Task 5's *verified on local Docker* walkthrough and
> Task 6's *verified: production boot path* — and nothing else. The full runbook —
> the local test flow, the VPS deploy procedure, and the troubleshooting table —
> is Task 7 and is not written yet. Until it is, `deploy.md` is still the only
> complete deploy procedure in this repo, and it is the systemd path.

## Verified on local Docker

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

### Prerequisites

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

### Step 1 — start the stack and seed

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

### Step 2 — the API port is NOT published

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

### Step 3 — the shipped relative-root default is not in use

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

### Step 4 — the proxy reaches the API

```bash
curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/v1/healthz
```

```
$ curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/v1/healthz
200
```

`000` here instead would mean Caddy cannot reach `api` — the failure mode if
`ADDR` were left at `127.0.0.1:8000`.

### Step 5 — the SPA is served with the right caching

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

### Step 6 — security headers

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

### Step 7 — data survives a restart

The plan's step 7 does not run as written, and **the worst version of it passes
silently**. One query names a column the schema does not have and dies; the
last one is a tautology that exits 0 while asserting nothing. Both are corrected
below, with the literal behaviour of each shown first.

#### (a) `otp_codes` is keyed on `phone`, not `phone_e164` — this one errors

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

#### (b) The last query is a tautology — this one silently passes

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

#### (c) The corrected step — asserts, and proves *that* user

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

#### (c2) `otp/start` fails silently once the rate limit is hit

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

#### (d) The corrected query has to be able to say "no"

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

### Step 8 — the data directory is host-owned, not root-owned

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

#### The real exposure: a missing photos directory is skipped in silence

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

### Step 9 — logs and rotation

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

## Verified: production boot path

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

### Step 1 — boot in production mode with placeholder credentials

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

### Step 2 — the API port is still closed in production mode

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

### Step 3 — `APP_DEBUG` is genuinely off

```bash
curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15551230000"}'
```

```
$ curl -sk -X POST https://localhost/v1/auth/otp/start -d '{"phone":"+15551230000"}'
{"ok":true}
```

No `devCode`. **That response alone does not prove `APP_DEBUG` is off.** The
handler has four distinct paths that each answer `{"ok":true}` silently
(`auth.go:120-146`, see §7(c2) above): the IP rate limit, a malformed phone, the
phone rate limit, and public-signup being off with the number un-allowlisted.
Only the fifth path — a code actually issued — omits `devCode` *because* debug is
off, and only it writes an OTP row.

So the discriminator is `otp_codes`:

```
$ sqlite3 data/gamematch.db "SELECT phone, code, code = '123456' AS is_dev_otp FROM otp_codes;"
+15551230000|518585|0
```

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

### Step 4 — a missing variable still fails the boot

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
is crashing roughly three times a second. **Use `docker inspect --format
'{{.State.ExitCode}}'`, or read the log — do not trust a single `ps` line here.**
The log naming the variable is the check that cannot be misread.

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

### Step 5 — restore local config

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

### Summary for the runbook

Two things an operator needs to know that the plan does not say:

1. **`.env` must not carry `DEV_OTP` into production, even in the local block.**
   The whole point of switching hosts is editing values in place, and `DEV_OTP`
   survives that edit. `Validate()` catches it, loudly — but the deploy fails
   first time with `DEV_OTP (must be unset...)`, which reads like a config bug
   rather than the documented "edit the local block into the VPS block"
   instruction. Delete the line, do not just change `APP_ENV`.
2. **A production misconfiguration shows as a crash loop, not as `Exited`.**
   Because of `restart: unless-stopped`, check the exit code with `docker
   inspect`, not `docker compose ps`. The symptom an operator will actually see
   on the site is a PWA that loads and an API that 502s.

Both belong in the VPS section as written.