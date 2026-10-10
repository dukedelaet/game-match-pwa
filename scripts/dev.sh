#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cd "$ROOT/apps/server"
export APP_DEBUG="${APP_DEBUG:-true}"

if [ ! -f data/gamematch.db ]; then
  echo "seeding database…"
  go run ./cmd/gamematch seed
fi

go run ./cmd/gamematch &
SERVER_PID=$!
trap 'kill "$SERVER_PID" 2>/dev/null || true' EXIT

cd "$ROOT/apps/web"
# Bind every interface by default so phones and other tailnet devices can reach
# the dev server. Set HOST=127.0.0.1 to keep it local-only.
exec pnpm dev --host "${HOST:-0.0.0.0}" --port "${PORT:-5173}"
