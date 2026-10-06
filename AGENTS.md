# AGENTS.md

GameMatch is a play-first dating PWA. Frontend is React; the backend is **Go + SQLite**.
There is **no PHP, no Laravel, and no MySQL** in this repo. If you see those words in
`docs/design-docs-and-wireframes.md`, they are historical design notes — the code is Go.

## Layout

| Path | What it is |
| --- | --- |
| `apps/web` | Vite + React 19 + TypeScript PWA (TanStack Router/Query, Zustand, Tailwind 4, `vite-plugin-pwa`) |
| `apps/server` | Go service. `cmd/gamematch` serves `/v1` (or `seed`); `internal/*` holds config, db, store, game engine, http handlers, media, seed |
| `packages/shared` | Shared TypeScript types |
| `docs` | Design spec, business proposal, runbooks, plans |

## Commands

```bash
# API (SQLite file at apps/server/data/gamematch.db, photos under apps/server/data/photos)
cd apps/server && go run ./cmd/gamematch          # serve on 127.0.0.1:8000
cd apps/server && go run ./cmd/gamematch seed      # idempotent demo data
cd apps/server && go test ./...                    # tests (add -race)
cd apps/server && go test -coverpkg=./... ./internal/...   # coverage

# Web
pnpm install
pnpm --filter web dev        # Vite on 127.0.0.1:5173, proxies /v1 -> :8000
pnpm --filter web build      # tsc -b && vite build
pnpm --filter web lint       # oxlint

# Both at once
bash scripts/dev.sh
```

Toolchain is managed by mise (`mise install`): Go and pnpm are pinned there.

## Conventions and gotchas

- **Contract:** the Go service must keep serving the existing `/v1` REST contract. The
  React app is unchanged by backend work; `vite.config.ts` proxies `/v1` to the Go server.
- **Auth:** opaque session cookie `gm_session` (`HttpOnly`, `SameSite=Lax`). No bearer
  token, no CSRF token for `/v1/*`.
- **Errors:** always `{"error":{"code":"...","message":"..."}}` with the same status codes.
- **Persistence:** SQLite in WAL mode, `sqlx` + hand-written SQL in `internal/store`.
  Schema is an embedded migration (`internal/db/migrations`, applied on boot via
  `PRAGMA user_version`). Upserts use `INSERT … ON CONFLICT(…) DO UPDATE`.
- **Timestamps** are RFC3339 UTC text (`store.NowTS`/`ParseTS`); IDs are UUID strings.
- **Game state** is request-driven: polls/answers call `Engine.Advance`; a 5s in-process
  ticker is only the forfeit/expiry backstop. No WebSocket.
- **Never expose** `dob`, `phone_e164`, `phone_e164_hash`, `approx_geohash`, or metro
  coordinates in API responses.
- **Dependencies:** `go.mod` pins chi, `sqlx`, `modernc.org/sqlite` (pure Go, no cgo),
  `uuid`, `imaging`, `testify`. Prefer stdlib + these over new libraries.
- **Package managers:** Go modules for `apps/server`, pnpm for the web workspace. Don't
  add another; pnpm is pinned via mise.
