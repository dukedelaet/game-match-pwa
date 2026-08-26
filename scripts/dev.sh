#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATADIR="$ROOT/.mysql"
export DB_PORT="${DB_PORT:-3307}"

if ! mysqladmin --socket="$DATADIR/mysql.sock" ping >/dev/null 2>&1; then
  if [ ! -d "$DATADIR/mysql" ]; then
    mysql_install_db --datadir="$DATADIR" --user="$(whoami)" --auth-root-authentication-method=normal
  fi
  mysqld --datadir="$DATADIR" --socket="$DATADIR/mysql.sock" --port="$DB_PORT" \
    --pid-file="$DATADIR/mysqld.pid" --bind-address=127.0.0.1 >/tmp/gamematch-mysql.log 2>&1 &
  sleep 2
  mysql --socket="$DATADIR/mysql.sock" -u root -e "CREATE DATABASE IF NOT EXISTS gamematch CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; CREATE USER IF NOT EXISTS 'gamematch'@'127.0.0.1' IDENTIFIED BY 'gamematch'; GRANT ALL ON gamematch.* TO 'gamematch'@'127.0.0.1'; FLUSH PRIVILEGES;"
fi

cd "$ROOT/apps/api"
php artisan serve --host=127.0.0.1 --port=8000 &
cd "$ROOT/apps/web"
exec pnpm dev --host 127.0.0.1 --port 5173
