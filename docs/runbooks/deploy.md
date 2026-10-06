# Deploy (Caddy + systemd)

The app is two artifacts: a static PWA (`apps/web/dist`) and one Go binary that
owns a SQLite file and the photo directory. Caddy serves both on one origin —
static files directly, `/v1/*` reverse-proxied to the binary on loopback.

```
browser ──TLS──> Caddy ──┬── /v1/*  ──> 127.0.0.1:8000 (gamematch)
                         └── /*     ──> apps/web/dist
```

## Build

```bash
bash scripts/deploy.sh
```

This installs deps, builds the PWA, runs the Go test suite, and writes
`apps/server/bin/gamematch`. It then prints the steps below.

## First-time host setup

```bash
sudo useradd --system --home /var/lib/gamematch --shell /usr/sbin/nologin gamematch
sudo install -d -o gamematch -g gamematch /var/lib/gamematch /opt/gamematch /etc/gamematch

sudo install -m755 apps/server/bin/gamematch /opt/gamematch/gamematch
sudo cp deploy/gamematch.env.example /etc/gamematch/gamematch.env
sudo chmod 600 /etc/gamematch/gamematch.env
sudoedit /etc/gamematch/gamematch.env          # set DATA_DIR, secrets

sudo cp deploy/gamematch.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now gamematch
```

## Caddy

```bash
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudoedit /etc/caddy/Caddyfile
```

Set these (Caddy reads them from its own environment or the `{env.*}` block):

| Variable | Meaning |
| --- | --- |
| `GAMEMATCH_SITE` | Public hostname. A real domain gets automatic HTTPS; unset serves plain HTTP on `:80`; `localhost` uses Caddy's internal CA. |
| `GAMEMATCH_ROOT` | Absolute path to `apps/web/dist`. |

```bash
sudo systemctl reload caddy
```

## Database

The SQLite file and photos live in `DATA_DIR`. Migrations run automatically on
boot (embedded, keyed on `PRAGMA user_version`); no migration step is needed.

**Demo data is for local hosts only** — `gamematch seed` inserts Alex, Jordan,
and a Staff account:

```bash
sudo -u gamematch env $(grep -v '^#' /etc/gamematch/gamematch.env | xargs) \
  /opt/gamematch/gamematch seed
```

Demo accounts (dev OTP `123456`, only while `APP_DEBUG=true`):

- Alex `+15551111111`
- Jordan `+15552222222`
- Staff `+15550000000` (admin)

## Verify

```bash
curl -fsS http://127.0.0.1:8000/v1/healthz     # {"ok":true}
curl -fsS https://YOUR_DOMAIN/v1/healthz
curl -fsSI https://YOUR_DOMAIN/ | head -1      # index.html
```

## Updating

```bash
git pull
bash scripts/deploy.sh
sudo install -m755 apps/server/bin/gamematch /opt/gamematch/gamematch
sudo systemctl restart gamematch
sudo systemctl reload caddy     # only if the Caddyfile changed
```

The PWA is served from the checkout, so a `pnpm --filter web build` is enough to
ship frontend changes — no service restart required.

## Operations

- Logs: `journalctl -u gamematch -f`; Caddy in `/var/log/caddy/gamematch-access.log`.
- Backups and restores: see `docs/runbooks/restore.md`.
- Optional integrations (Twilio, Google/Apple OAuth, Turnstile, Web Push) are
  configured through `/etc/gamematch/gamematch.env`; each degrades to a logged
  no-op when its variables are absent.
