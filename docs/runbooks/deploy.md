# Deploy (Caddy + systemd) — legacy

> **Status: legacy. Do not deploy a new host from this file.** The supported
> deployment path is Docker Compose: **[`deploy-docker.md`](deploy-docker.md)**.
>
> | If you want to… | Read |
> | --- | --- |
> | Deploy GameMatch to a VPS | [`deploy-docker.md`](deploy-docker.md) |
> | Deploy it on your own machine | [`deploy-docker.md`](deploy-docker.md) — same compose file, a different `.env` |
> | Restore from a backup | [`restore.md`](restore.md) |
> | Keep an existing systemd deployment running | This file, below |
> | Check this repo's current backup schedule | [Backups](deploy-docker.md#backups) |
>
> **What changed.** The systemd path is superseded by one `docker compose` file:
> `api`, `web` (Caddy) and a scheduled `backup` container. It installs Go and
> pnpm on the host, it puts Caddy's and the API's configuration in three
> different places, and it has been superseded by the compose stack for both
> local and production use. It still works, and the instructions below are
> unchanged and accurate for the units in `deploy/` — but they are no longer
> maintained as the primary path, and a host moved from systemd to Compose must
> change `DATA_DIR`, the backup directory, and the process that stops the server
> during a restore.
>
> **Do not run both on one host.** They would fight over ports 80, 443 and
> 8000, and two schedulers would write backups in two different formats to two
> different directories.

The rest of this file documents the systemd deployment as it was built. The app
is two artifacts: a static PWA (`apps/web/dist`) and one Go binary that owns a
SQLite file and the photo directory. Caddy serves both on one origin — static
files directly, `/v1/*` reverse-proxied to the binary on loopback.

```
browser ──TLS──> Caddy ──┬── /v1/*  ──> 127.0.0.1:8000 (gamematch)
                         └── /*     ──> apps/web/dist
```

## The systemd path (legacy)

### Build

```bash
bash scripts/deploy.sh
```

This installs deps, builds the PWA, runs the Go test suite, and writes
`apps/server/bin/gamematch`. It then prints the steps below.

### First-time host setup

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

### Caddy

```bash
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudoedit /etc/caddy/Caddyfile
```

Set these (Caddy reads them from its own environment or the `{env.*}` block):

| Variable | Meaning |
| --- | --- |
| `GAMEMATCH_SITE` | Public hostname. A real domain gets an automatic certificate; the default, `localhost`, serves HTTPS on `:443` with Caddy's internal CA. **There is no plain-HTTP mode.** |
| `GAMEMATCH_ROOT` | Absolute path to `apps/web/dist`. Default `/srv/dist`. Must be absolute — a relative path resolves against the working directory and every asset 404s while `/v1/healthz` still returns 200. |
| `GAMEMATCH_API` | Address the API is proxied to. Default `127.0.0.1:8000`, correct on a host; the Compose stack sets `api:8000`. |
| `GAMEMATCH_LOG_DIR` | Directory for the access log. Default `/var/log/caddy`. Caddy creates it if absent, but it must be writable by the Caddy user. |

```bash
sudo systemctl reload caddy
```

### Database

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

### Verify

```bash
curl -fsS http://127.0.0.1:8000/v1/healthz     # {"ok":true}
curl -fsS https://YOUR_DOMAIN/v1/healthz
curl -fsSI https://YOUR_DOMAIN/ | head -1      # index.html
```

### Updating

```bash
git pull
bash scripts/deploy.sh
sudo install -m755 apps/server/bin/gamematch /opt/gamematch/gamematch
sudo systemctl restart gamematch
sudo systemctl reload caddy     # only if the Caddyfile changed
```

The PWA is served from the checkout, so a `pnpm --filter web build` is enough to
ship frontend changes — no service restart required.

### Operations

- Logs: `journalctl -u gamematch -f`; Caddy in `/var/log/caddy/gamematch-access.log`.
- Backups: `gamematch-backup.timer` runs `scripts/backup.sh` **hourly** into
  `/backup/gamematch`, keeping 14 days — a one-hour recovery point objective.
  (The Compose stack's `backup` service runs the same script **every 6 hours**,
  so its RPO is up to six; see [`deploy-docker.md` § Backups](deploy-docker.md#backups).)
  Check it is actually running:
  `systemctl list-timers gamematch-backup.timer` and `journalctl -u gamematch-backup`.
  Backups are local to the host; copy them off-box, or a disk failure takes the
  history with it. Restores: see `docs/runbooks/restore.md`.
- SMS delivery and Turnstile are **required** in production: the service refuses
  to start without them rather than logging one-time codes to the journal. Web
  Push (VAPID) stays optional; generate a pair with `gamematch vapid`.

## Moving a systemd host onto the Compose stack

There is no in-place migration. Plan for a short outage:

1. Take a fresh backup with `systemctl start gamematch-backup.service`, and copy
   the resulting `gamematch-<stamp>.db` and `photos-<stamp>.tar.gz` off the old
   host. Those are the same artefacts the Compose stack writes, so they restore
   either way.
2. On the new host, follow [`deploy-docker.md`](deploy-docker.md) to the point
   where the stack is running and `docker compose run --rm api seed` has been
   run. `seed` is idempotent and will overwrite the seeded demo rows, which is
   why step 1 must be the *pre*-seed copy.
3. Stop the old service (`systemctl stop gamematch`), then follow the Compose
   procedure in [`restore.md`](restore.md) against the new host.
4. Only once the new host serves traffic, stop and disable the old units:
   `systemctl disable --now gamematch gamematch-backup.timer caddy`.

Do not skip step 4 on a shared host: leaving the systemd units enabled leaves two
Caddys competing for ports 80 and 443.
