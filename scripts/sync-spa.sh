#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/apps/web/dist"
DEST="$ROOT/apps/api/public"
if [[ ! -d "$DIST" ]]; then
  echo "missing $DIST — run pnpm --filter web build first" >&2
  exit 1
fi
rsync -a --delete \
  --exclude 'index.php' \
  --exclude '.htaccess' \
  --exclude 'robots.txt' \
  --exclude 'favicon.ico' \
  "$DIST/" "$DEST/"
test -f "$DEST/index.html"
echo "SPA copied to $DEST"
