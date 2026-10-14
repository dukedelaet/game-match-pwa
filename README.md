# GameMatch

Play-first dating PWA. React front end, Go + SQLite backend.

## Local

```bash
# requires Go 1.27.2+ and pnpm; `mise install` picks up the pinned toolchain
pnpm install

cd apps/server && go run ./cmd/gamematch seed   # first run only
go run ./cmd/gamematch                          # http://127.0.0.1:8000

# other terminal
cd apps/web && pnpm dev
```

Or run everything with `bash scripts/dev.sh` (starts the Go API on 8000 and Vite on 5173;
seeds on first run).

Open http://localhost:5173

Demo accounts (code **123456**):

- Alex `+15551111111`
- Jordan `+15552222222`
- Staff `+15550000000` (admin: reports + invite list)

## Layout

- `apps/web` — Vite + React 19 PWA (TanStack Router, Zustand, Tailwind)
- `apps/server` — Go `/v1` API over SQLite (`cmd/gamematch` serves; pass `seed` to seed)
- `docker-compose.yml` — the supported deployment: API, Caddy-served PWA, backups
- `docs/` — design spec and business proposal

## Deploy

**[`docs/runbooks/deploy-docker.md`](docs/runbooks/deploy-docker.md)** — the
supported path. One `docker compose up --build -d` brings up the API, the PWA
behind Caddy with automatic HTTPS, and a scheduled backup; the same file covers
a VPS and a local machine, which differ only in `.env`.

Build the PWA before the first image build — the `web` image copies
`apps/web/dist` rather than building it:

```bash
pnpm --filter web build
```

`docs/runbooks/deploy.md` is the older systemd path, kept for hosts already
running it.

## API

The Go service serves the same `/v1` REST contract the React app already calls,
so `vite.config.ts` proxies `/v1` to `127.0.0.1:8000` unchanged. Auth is a
cookie session (`gm_session`); OTP codes are returned as `devCode` when
`APP_DEBUG=true`.

Environment: `ADDR`/`PORT` (default `127.0.0.1:8000`), `DATA_DIR` (default
`apps/server/data`), `APP_ENV`, `APP_DEBUG`, `HOUSE_USER_ID`, `DEV_OTP`.

With `APP_ENV=production` the service refuses to start unless `APP_DEBUG` is
false, `DEV_OTP` is unset, and `SMS_PROVIDER` plus the Twilio and Turnstile
credentials are all configured. Signing in is gated on the `auth.otp` and
`auth.public_signup` feature flags, which migration `0008` seeds with safe
defaults.

## Tests

```bash
cd apps/server && go test ./...
cd apps/web && pnpm test       # Vitest
```
