# GameMatch

Play-first dating PWA. React front end, Laravel + MySQL on Hostinger-style hosting.

## Local

```bash
# MySQL on 3307 (user-space MariaDB or docker compose)
docker compose up -d   # optional if port 3307 is free
cd apps/api && composer install && cp -n .env.example .env && php artisan key:generate
php artisan migrate:fresh --seed
php artisan serve --port=8000
# other terminal
cd apps/web && pnpm install && pnpm dev
```

Or `bash scripts/dev.sh` after migrate.

Open http://localhost:5173

Production deploys from the **`release`** branch via GitHub Actions (not from `main`). See `docs/runbooks/deploy.md` and `docs/runbooks/restore.md`.

Demo accounts (code **123456**):

- Alex `+15551111111`
- Jordan `+15552222222`
- Staff `+15550000000` (admin: reports + invite list)

## Layout

- `apps/web` — Vite + React 19 PWA
- `apps/api` — Laravel `/v1` API
- `docs/` — design spec and business proposal
