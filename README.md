# GameMatch

Play-first dating PWA. React front end, Go + SQLite backend.

## Local

```bash
# requires Go 1.24+ and pnpm; `mise install` picks up the pinned toolchain
cd apps/web && pnpm install

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
- `docs/` — design spec and business proposal

## API

The Go service serves the same `/v1` REST contract the React app already calls,
so `vite.config.ts` proxies `/v1` to `127.0.0.1:8000` unchanged. Auth is a
cookie session (`gm_session`); OTP codes are returned as `devCode` when
`APP_DEBUG=true`.

Environment: `ADDR`/`PORT` (default `127.0.0.1:8000`), `DATA_DIR` (default
`apps/server/data`), `APP_ENV`, `APP_DEBUG`, `HOUSE_USER_ID`, `DEV_OTP`.

## Tests

```bash
cd apps/server && go test ./...
```
