#!/usr/bin/env bash
# Build the PWA and the server binary, then print the operator steps.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "==> installing workspace deps"
(cd "$ROOT" && pnpm install --frozen-lockfile)

echo "==> building the PWA"
(cd "$ROOT" && pnpm --filter web build)

echo "==> running server tests"
(cd "$ROOT/apps/server" && go test ./...)

echo "==> building the server binary"
mkdir -p "$ROOT/apps/server/bin"
(cd "$ROOT/apps/server" && CGO_ENABLED=0 go build -trimpath -o bin/gamematch ./cmd/gamematch)

echo
echo "Built:"
echo "  PWA    $ROOT/apps/web/dist"
echo "  server $ROOT/apps/server/bin/gamematch"
echo
cat <<'STEPS'
Operator steps (once per host):

  1. create the service user and data dir
       sudo useradd --system --home /var/lib/gamematch --shell /usr/sbin/nologin gamematch
       sudo install -d -o gamematch -g gamematch /var/lib/gamematch /opt/gamematch /etc/gamematch

  2. install the binary and env file
       sudo install -m755 apps/server/bin/gamematch /opt/gamematch/gamematch
       sudo cp deploy/gamematch.env.example /etc/gamematch/gamematch.env
       sudo chmod 600 /etc/gamematch/gamematch.env
       sudoedit /etc/gamematch/gamematch.env

  3. install and start the service
       sudo cp deploy/gamematch.service /etc/systemd/system/
       sudo systemctl daemon-reload
       sudo systemctl enable --now gamematch

  4. point Caddy at the built PWA
       sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
       sudoedit /etc/caddy/Caddyfile      # set GAMEMATCH_SITE and GAMEMATCH_ROOT
       sudo systemctl reload caddy

  5. verify
       curl -fsS http://127.0.0.1:8000/v1/healthz
       curl -fsS https://YOUR_DOMAIN/v1/healthz

Seeding demo data (local/demo hosts only):
       sudo -u gamematch env $(cat /etc/gamematch/gamematch.env | grep -v '^#') \
         /opt/gamematch/gamematch seed
STEPS
