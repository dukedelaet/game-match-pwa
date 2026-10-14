#!/usr/bin/env bash
# Verifies deploy/Caddyfile against a built apps/web/dist, without touching
# the host's caddy binary (Caddy 2.10.2 is incompatible with Go 1.27 and
# panics on any config) and without needing a real domain.
#
# Usage: bash deploy/caddy-verify.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${CADDY_VERIFY_PORT:-9444}"
LOGDIR="$ROOT/.caddytest"
WORKDIR="$ROOT/apps/web/dist"

# Paths as the *container* sees them. GAMEMATCH_ROOT and GAMEMATCH_LOG_DIR are
# read by Caddy inside the image, so a host path would not resolve there --
# which is exactly the relative/absolute-root defect this config had.
CTOR_DIST=/srv/dist
CTOR_LOGDIR=/var/log/caddy-test

# Docker needs the `docker` group, and a shell session started after joining
# it; older sessions get "permission denied ... /var/run/docker.sock". Retry
# through newgrp so the harness runs either way.
docker_run() {
  if docker info >/dev/null 2>&1; then
    docker "$@"
  else
    newgrp docker <<< "docker $*"
  fi
}

# Always tear the container and the log dir down, so a failed run does not
# leave a published port or an untracked .caddytest behind.
cleanup() {
  docker_run rm -f gm-caddy-verify >/dev/null 2>&1 || true
  rm -rf "$LOGDIR"
}
trap cleanup EXIT

fail=0
check() { # check <label> <expected> <actual>
  if [ "$2" = "$3" ]; then printf '  ok   %s\n' "$1"
  else printf '  FAIL %s: expected %s, got %s\n' "$1" "$2" "$3"; fail=1; fi
}
# Caddy's internal CA issues a certificate for the site name, so a request to
# the bare IP completes no SNI handshake at all. Probe by name, pinned to the
# loopback address so DNS order cannot pick a different interface.
BASE="https://localhost:$PORT"
CURL=(curl -sk --resolve "localhost:$PORT:127.0.0.1")
header() { # header <path> <header>  -> value or ""
  "${CURL[@]}" -o /dev/null -D - "$BASE$1" 2>/dev/null \
    | tr -d '\r' | awk -v h="$2:" 'tolower($1)==tolower(h){$1="";sub(/^ /,"");print;exit}'
}
status() { "${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE$1"; }

if [ ! -f "$WORKDIR/index.html" ]; then
  echo "caddy-verify: $WORKDIR/index.html missing; run 'pnpm --filter web build' first" >&2
  exit 1
fi

rm -rf "$LOGDIR"; mkdir -p "$LOGDIR"

# A container left behind by a killed run would collide on the name.
docker_run rm -f gm-caddy-verify >/dev/null 2>&1 || true

# -e rather than a shell prefix: the newgrp path re-parses these arguments as
# a command line, and shell prefixes would not survive that. The log directory
# is mounted somewhere other than /var/log/caddy, so a hardcoded path in the
# config shows up as a failure instead of passing by coincidence.
docker_run run -d --name gm-caddy-verify -p "$PORT:$PORT" \
  -e GAMEMATCH_ROOT="$CTOR_DIST" -e GAMEMATCH_SITE="localhost:$PORT" \
  -e GAMEMATCH_LOG_DIR="$CTOR_LOGDIR" \
  -v "$ROOT/deploy/Caddyfile:/etc/caddy/Caddyfile:ro" \
  -v "$WORKDIR:$CTOR_DIST:ro" \
  -v "$LOGDIR:$CTOR_LOGDIR" \
  caddy:2.10.2-alpine caddy run --config /etc/caddy/Caddyfile >/dev/null

# Wait for the listener rather than sleeping a fixed amount.
for _ in $(seq 1 40); do
  if [ "$(status /)" != "000" ]; then break; fi
  sleep 0.25
done

echo "caddy-verify:"
check "GET / serves the shell"            200 "$(status /)"
check "GET /boot (PWA start_url) serves"   200 "$(status /boot)"
check "GET /deep/spa/route falls back"     200 "$(status /deep/spa/route)"
check "/boot is not cached"                "no-cache" "$(header /boot Cache-Control)"

asset="$(ls "$WORKDIR/assets" | grep -m1 '\.js$')"
check "asset is immutable"                 "public, max-age=31536000, immutable" \
      "$(header "/assets/$asset" Cache-Control)"
check "nosniff present"                    "nosniff" "$(header / X-Content-Type-Options)"
check "CSP present"                        "1" "$(test -n "$(header / Content-Security-Policy)" && echo 1 || echo 0)"
check "HSTS present"                       "1" "$(test -n "$(header / Strict-Transport-Security)" && echo 1 || echo 0)"
check "X-Frame-Options present"            "1" "$(test -n "$(header / X-Frame-Options)" && echo 1 || echo 0)"
check "access log honours \$GAMEMATCH_LOG_DIR" "1" \
      "$(test -s "$LOGDIR/gamematch-access.log" && echo 1 || echo 0)"

exit "$fail"