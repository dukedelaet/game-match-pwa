# Handoff — first VPS deploy of GameMatch

**State at handoff:** branch `main` is clean, matches `origin/main` at
`c24cf9b` (`docs: close the whole-branch review findings`). All work is merged
and pushed. Nothing is left to do on this machine.

**Goal:** stand up the Docker Compose stack on the VPS.
**Authoritative reference:** `docs/runbooks/deploy-docker.md` (this file is a
condensed companion — if anything disagrees, the runbook wins).

---

## 0. What you need before you start

- [ ] VPS with Docker Engine + Compose v2 (`docker compose version` works)
- [ ] `git`, outbound network to GitHub and Docker Hub
- [ ] `mise`, `pnpm` (or Node 24 + pnpm 12.9.1) — needed to build the PWA on the host
- [ ] Domain A/AAAA record **already points at the VPS public IP** and has propagated:
      `getent hosts your.domain` prints the VPS IP. If not, STOP. Caddy issues
      the TLS cert on first boot and there is no plain-HTTP fallback.
- [ ] Inbound TCP **80 and 443** open (firewall + any cloud security group)
- [ ] Outbound TCP **443** open (the `backup` container runs `apk add` on every start)
- [ ] Twilio: Account SID (`AC…`), Auth Token, From number (`+1…`)
- [ ] Cloudflare Turnstile **secret** key (not the site key)

---

## 1. Get the code

```bash
git clone https://github.com/dukedelaet/game-match-pwa.git
cd game-match-pwa
git log --oneline -1     # expect c24cf9b
```

## 2. Create and fill `.env`

```bash
cp .env.example .env

# swap the local block for the VPS block (delimiter is |, not /)
sed -i 's|^\([A-Z][A-Z_0-9]*=\)|#\1|' .env
sed -i '/^# --- VPS:/,/^# --- optional:/{/^#DEV_OTP=/!{/^#[A-Z][A-Z_0-9]*=/s/^#//}}' .env
```

Then edit `.env` and fill in:

| Key | Value |
| --- | --- |
| `GAMEMATCH_SITE` | your real domain (NOT `gamematch.example.com`) |
| `TWILIO_ACCOUNT_SID` | from Twilio console |
| `TWILIO_AUTH_TOKEN` | from Twilio console |
| `TWILIO_FROM` | E.164, e.g. `+15550000000` |
| `TURNSTILE_SECRET` | Turnstile secret key |
| `APP_ENV` | must stay `production` |
| `DEV_OTP` | **must stay commented out** (production refuses to boot if set) |

Verify before continuing — each command should print **nothing**:

```bash
grep -E '^[A-Z]' .env | cut -d= -f1 | sort | uniq -d   # no duplicate keys
grep -E '^[A-Z][A-Z_0-9]*=$' .env                       # no empty values
grep -E '^DEV_OTP=' .env                                # DEV_OTP absent
```

## 3. Data dirs (API runs as UID 10001)

```bash
mkdir -p data backups
sudo chown -R 10001:10001 data
ls -ldn data            # expect "10001 10001"
```

## 4. Build the PWA (required — web image builds nothing)

```bash
pnpm install
pnpm --filter web build
```

## 5. Start the stack

```bash
docker compose up --build -d
```

**Do NOT run `seed`** — demo users/known OTP are for local only. Skip it.

## 6. Verify

```bash
docker compose ps
docker inspect --format '{{.RestartCount}}' gamematch-api-1        # 0
docker compose logs api | tail -5                                  # debug=false
ls -l backups/                                                     # .db appears within ~3 min

curl -fsS https://your.domain/v1/healthz                           # {"ok":true}
curl -fsSI https://your.domain/boot | head -1                      # HTTP/2 200

# API must NOT be reachable on the host — expect 000 (refused):
curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:8000/v1/healthz
```

Finally: **sign in from a real phone** — the only check that exercises Twilio +
Turnstile together.

---

## Gotchas that cause the deploy to fail

- `APP_ENV=production` + any `DEV_OTP` → API crash-loops with
  `config: unsafe production configuration: DEV_OTP`. Leave `DEV_OTP` commented.
- Duplicate key in `.env` (e.g. appended value) → ambiguous file. Fill values on
  the existing line, don't append.
- DNS not propagated at first boot → Caddy keeps retrying; no plain-HTTP fallback.
- Never `docker compose down -v` → deletes `caddy-data` (ACME certs). Let's
  Encrypt rate-limits 5 duplicate certs/week. Plain `down` is safe.

## Updating later

```bash
git pull
pnpm --filter web build            # only if frontend changed
docker compose up --build -d
# API only:       docker compose up -d --build api
# web only:       docker compose up -d --build web
# .env change:    docker compose up -d --force-recreate api
```

Migrations run automatically on boot (embedded, keyed on `PRAGMA user_version`).
Rollback = `git checkout <prev-sha>`, rebuild, `up -d`; schema never goes backwards,
so roll forward instead.

## Backups

- `backup` runs on start then every 6h → `./backups/gamematch-<stamp>.db` (+
  `photos-<stamp>.tar.gz` when photos exist), 14-day retention.
- `./backups` is on the **same disk** as `./data` — copy off-box or it won't
  survive a disk failure.
- On-demand backup: `docker compose exec backup /scripts/backup.sh /backups`
  (run through the service's own entrypoint, not `--entrypoint`).

Restore procedure: `docs/runbooks/restore.md`.
