# Docker deployment

> **Status: partial.** This file currently holds only the *verified on local
> Docker* evidence recorded by the deployment plan's Task 5. The full runbook —
> the local test flow, the VPS deploy procedure, and the troubleshooting table —
> is Task 7 and is not written yet. Until it is, `deploy.md` is still the only
> complete deploy procedure in this repo, and it is the systemd path.

## Verified on local Docker

Recorded on a local machine on 2026-10-14, against the stack as committed at
this point in the plan (Tasks 1–4). Everything below is the real terminal
output, unedited, with the real numbers.

Two things about how to read it:

- Caddy serves HTTPS with its **internal CA**, so `curl -k` appears throughout.
  That is expected; there is no plain-HTTP mode.
- The API container is deliberately **not** published to the host, so it has no
  URL. Everything below reaches it through Caddy on `https://localhost`, or from
  inside the compose network.

### Prerequisites

```bash
cp .env.example .env          # APP_ENV=local in the local block
mkdir -p data backups
sudo chown -R 10001:10001 data
```

`sudo` needs a password, so `./data` was made uid 10001 with a throwaway
container instead — same result, no host credential:

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

(wlp0s20f0u1i2, tailscale0, incusbr0, docker0 and the three compose bridges.)

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

The plan's step 7 does not run as written. Two of its three queries name
columns the schema does not have, and the last one asserts something that is
false on the day of a first sign-in. All three corrections are below, with the
literal failure first.

**(a) The plan's queries, verbatim — fails:**

```bash
CODE="$(sqlite3 data/gamematch.db "SELECT code FROM otp_codes WHERE phone_e164 = '+15551239999';")"
```

```
Parse error in 2nd command line argument: no such column: phone_e164
  SELECT code FROM otp_codes WHERE phone_e164 = '+15551239999';
                     error here ---^
could not read the local OTP; is APP_ENV=local?
```

`otp_codes` is keyed on `phone`, not `phone_e164`:

```bash
$ sqlite3 data/gamematch.db ".schema otp_codes"
CREATE TABLE otp_codes (
    phone      TEXT PRIMARY KEY,
    code       TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
```

The plan's last query has the same problem — `user_private` has no
`phone_e164_hash`; `users` does, and the two join on `user_id`:

```bash
$ sqlite3 data/gamematch.db ".schema user_private"
CREATE TABLE user_private (
    user_id      TEXT PRIMARY KEY,
    phone_e164   TEXT,
    created_at   TEXT,
    updated_at   TEXT
);
```

One more wart in the same step: `test -n "$CODE"` comes *after* the verify call
that consumes `$CODE`, so an unreadable OTP is only detected after it has
already been spent.

**(b) Corrected, and passing.** The assert moved before its use, and the user is
identified by id rather than by name — signing in creates the row but does not
set a name, so a fresh user has `name IS NULL` and the plan's "printing a name"
cannot hold on the day of a first sign-in:

```bash
set -x   # so the trace below shows which value each step actually used
BEFORE="$(sqlite3 data/gamematch.db 'SELECT count(*) FROM users;')"

curl -sk -X POST https://localhost/v1/auth/otp/start  -d '{"phone":"+15551239999"}' -o /dev/null
CODE="$(sqlite3 data/gamematch.db "SELECT code FROM otp_codes WHERE phone = '+15551239999';")"
test -n "$CODE" || { echo "could not read the local OTP; is APP_ENV=local?"; exit 1; }
curl -sk -X POST https://localhost/v1/auth/otp/verify -d "{\"phone\":\"+15551239999\",\"code\":\"$CODE\"}" -o /dev/null

docker compose restart api
AFTER="$(sqlite3 data/gamematch.db 'SELECT count(*) FROM users;')"
printf 'before=%s after=%s\n' "$BEFORE" "$AFTER"
test "$AFTER" -gt "$BEFORE" || { echo "FAIL: the user did not survive the restart"; exit 1; }
```

```
+ BEFORE=4
+ CODE=123456
+ test -n 123456
+ curl -sk -X POST https://localhost/v1/auth/otp/verify -d '{"phone":"+15551239999","code":"123456"}' -o /dev/null
+ docker compose restart api
 Container gamematch-api-1 Restarting
 Container gamematch-api-1 Started
+ AFTER=5
+ printf 'before=%s after=%s\n' 4 5
before=4 after=5
+ test 5 -gt 4
```

The local OTP is `123456`, readable out of `./data` because `APP_ENV=local`.

**(c) The same claim asserted by identity rather than by count.** A count going
up proves *a* user exists after the restart; it does not prove it is the one
that was created. Same row, same `created_at`, across the restart:

```
user id=2c15e3d6-5dbd-40a0-a7f5-cdf8c1f282e9 created_at=2026-10-14T21:50:47.127419Z
after restart: id=2c15e3d6-5dbd-40a0-a7f5-cdf8c1f282e9 created_at=2026-10-14T21:50:47.127419Z
2c15e3d6-5dbd-40a0-a7f5-cdf8c1f282e9|pending|intent|2026-10-14T21:50:47.127419Z

$ sqlite3 data/gamematch.db 'PRAGMA journal_mode;'
wal
```

`journal_mode` is `wal`, so the restart above ran against a live WAL database on
the bind mount — which is the point of the exercise. A host filesystem that
cannot support WAL locking fails here rather than silently later.

Signing in end to end through Caddy also works, opaque `HttpOnly` cookie and
all:

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

**`data/photos` does not exist on a fresh stack, and the plan's command fails
on it.** Immediately after a first `up` and `seed`:

```
$ ls -ld data data/photos
ls: cannot access 'data/photos': No such file or directory
drwxr-xr-x 1 10001 10001 88 Oct 14 14:51 data
```

The image builds `install -d -o 10001 -g 10001 /data/photos`, but `./data` is a
bind mount, so the host directory shadows the image's `/data` and takes the
`photos` subdirectory with it. It is created on demand instead, by the upload
handler's `media.MkdirAll` (`apps/server/internal/httpapi/photos.go:42`). Uploading
one photo through the stack creates it, owned by 10001:

```
$ curl -sk -b jar -X POST https://localhost/v1/me/photos \
    -F "photo=@apps/web/dist/icons/icon-192.png;type=image/png"
{"photo":{"id":"4002089b-d1ad-4e9d-a32e-9d22d8e7e160","url":"/v1/photos/4002089b-d1ad-4e9d-a32e-9d22d8e7e160","state":"ok"}}

$ ls -ld data data/photos
drwxr-xr-x 1 10001 10001 100 Oct 14 14:51 data
drwxr-x--- 1 10001 10001  72 Oct 14 14:51 data/photos
```

So the ownership property holds, but the plan's `ls -ld data data/photos` check
is a false alarm on a fresh install. Note the mode: `0750`, so the host user
cannot `find data/photos` — that is `media.dirPerm` by design, not a
permissions bug.

The same absence has a visible effect on backups. `scripts/backup.sh` guards the
archive with `if [ -d "$DATA_DIR/photos" ]`, so the first backup of a fresh
install is database-only:

```
$ ls -la backups/
total 504
drwxr-xr-x 1 duke duke     58 Oct 14 14:50 .
drwxr-xr-x 1 duke duke    438 Oct 14 14:49 ..
-rw-r--r-- 1 root root 516096 Oct 14 14:50 gamematch-20261014T215031Z.db

$ docker compose run --rm --entrypoint sh backup \
    -c "apk add --no-cache bash sqlite >/dev/null && /scripts/backup.sh /backups"
backup: wrote /backups/gamematch-20261014T215152Z.db

$ ls -la backups/
total 1020
drwxr-xr-x 1 duke duke    176 Oct 14 14:51 .
drwxr-xr-x 1 duke duke    438 Oct 14 14:49 ..
-rw-r--r-- 1 root root 516096 Oct 14 14:50 gamematch-20261014T215031Z.db
-rw-r--r-- 1 root root 516096 Oct 14 14:51 gamematch-20261014T215152Z.db
-rw-r--r-- 1 root root  8988 Oct 14 14:51 photos-20261014T215152Z.tar.gz
```

The scheduled run reported:

```
$ docker compose logs --tail 12 backup
backup-1  | backup: wrote /backups/gamematch-20261014T215031Z.db
```

and the container reached `healthy` on its own.

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
`"admin": {"disabled": true}` — `deploy/Caddyfile` turns the admin API off, so
port 2019 in `docker compose ps` is the image's `EXPOSE` and nothing is
listening:

```
$ docker compose exec web wget -qO- http://127.0.0.1:2019/config/
wget: can't connect to remote host (127.0.0.1): Connection refused
```

Use `caddy adapt` or `docker compose exec web caddy reload` rather than the admin
API when reading or changing a running config.