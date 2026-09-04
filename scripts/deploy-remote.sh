#!/usr/bin/env bash
set -euo pipefail

: "${DOMAIN_PATH:?DOMAIN_PATH required}"
: "${SHA:?SHA required}"
: "${PHP_BIN:?PHP_BIN required}"
: "${PRODUCTION_URL:?PRODUCTION_URL required}"

DOMAIN_PATH="${DOMAIN_PATH%/}"
RELEASE="$DOMAIN_PATH/releases/$SHA"
CURRENT="$DOMAIN_PATH/current"
SHARED="$DOMAIN_PATH/shared"
HTML="$DOMAIN_PATH/public_html"
PHP="$PHP_BIN"

test -f "$SHARED/.env" || { echo "missing $SHARED/.env" >&2; exit 1; }
test -d "$RELEASE" || { echo "missing release $RELEASE" >&2; exit 1; }

ln -sfn "$SHARED/.env" "$RELEASE/.env"
ln -sfn "$SHARED/storage" "$RELEASE/storage"

cd "$RELEASE"
"$PHP" artisan package:discover --ansi
"$PHP" artisan migrate --force
"$PHP" artisan config:cache
"$PHP" artisan route:cache
"$PHP" artisan view:cache

test -f "$RELEASE/public/index.html" || { echo "SPA index.html missing in release" >&2; exit 1; }

PREV="$(readlink "$CURRENT" 2>/dev/null || true)"
ln -sfn "$RELEASE" "$CURRENT"

retarget_public_html() {
  local target="$CURRENT/public"
  if [[ ! -e "$HTML" ]] || [[ -L "$HTML" ]]; then
    ln -sfn "$target" "$HTML"
    return
  fi
  if [[ -d "$HTML" ]]; then
    if [[ -f "$HTML/index.php" ]] && grep -q 'current/public/index.php' "$HTML/index.php" 2>/dev/null; then
      rsync -a --exclude 'index.php' --exclude '.htaccess' "$target/" "$HTML/"
      return
    fi
    if [[ -f "$HTML/wp-config.php" ]] || { [[ -f "$HTML/index.php" ]] && grep -q 'vendor/autoload.php' "$HTML/index.php"; }; then
      echo "refusing to replace WordPress or copied Laravel public_html" >&2
      exit 1
    fi
    mv "$HTML" "$HTML.bak.$(date +%s)"
    ln -sfn "$target" "$HTML"
  fi
}

retarget_public_html
ln -sfn "$SHARED/storage/app/public" "$CURRENT/public/storage" 2>/dev/null || true

rollback() {
  echo "health check failed" >&2
  if [[ -n "${PREV}" && -d "$PREV" && "$PREV" == "$DOMAIN_PATH/releases/"* ]]; then
    ln -sfn "$PREV" "$CURRENT"
    if [[ -L "$HTML" ]]; then
      ln -sfn "$CURRENT/public" "$HTML"
    fi
  fi
  exit 1
}

curl -fsS "$PRODUCTION_URL/up" >/dev/null || rollback
health="$(curl -fsS "$PRODUCTION_URL/v1/healthz" || true)"
echo "$health" | grep -q '"ok":true' || rollback
home="$(curl -fsS "$PRODUCTION_URL/" || true)"
echo "$home" | grep -Eqi 'id="root"|GameMatch' || rollback

cd "$DOMAIN_PATH/releases"
ls -1dt */ 2>/dev/null | tail -n +6 | while read -r old; do
  rm -rf "$old"
done

echo "deployed $SHA"
