#!/usr/bin/env bash
# Back up the GameMatch database and photos.
#
# The service runs SQLite in WAL mode, so the database file alone is not a
# consistent snapshot: anything not yet checkpointed lives in the -wal
# sidecar. Copying only the .db while the server runs restores a database
# missing its own schema. sqlite3's online backup API takes a consistent
# snapshot against a live database instead.
#
# Usage: scripts/backup.sh [backup-dir]   (default /backup/gamematch)

set -euo pipefail

BACKUP_DIR="${1:-/backup/gamematch}"
DATA_DIR="${DATA_DIR:-/var/lib/gamematch}"

command -v sqlite3 >/dev/null 2>&1 || {
	echo "backup: sqlite3 is required but not installed" >&2
	exit 1
}

if [ ! -f "$DATA_DIR/gamematch.db" ]; then
	echo "backup: no database at $DATA_DIR/gamematch.db" >&2
	exit 1
fi

mkdir -p "$BACKUP_DIR"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DB_OUT="$BACKUP_DIR/gamematch-$STAMP.db"
PHOTOS_OUT="$BACKUP_DIR/photos-$STAMP.tar.gz"

# Online backup: consistent without stopping the service or locking writers.
sqlite3 "$DATA_DIR/gamematch.db" ".backup '$DB_OUT'"

# Verify the snapshot before trusting it. A truncated or unreadable backup is
# worse than none, because it is found at restore time.
if ! sqlite3 "$DB_OUT" 'PRAGMA integrity_check;' | grep -qx ok; then
	echo "backup: integrity check failed for $DB_OUT" >&2
	rm -f "$DB_OUT"
	exit 1
fi
if ! sqlite3 "$DB_OUT" 'SELECT count(*) FROM sqlite_master;' | grep -qE '^[1-9]'; then
	echo "backup: snapshot has no schema" >&2
	rm -f "$DB_OUT"
	exit 1
fi

if [ -d "$DATA_DIR/photos" ]; then
	tar czf "$PHOTOS_OUT" -C "$DATA_DIR" photos
fi

# Keep 14 days. Runs hourly, so this is two weeks of history.
find "$BACKUP_DIR" -name 'gamematch-*.db' -mtime +14 -delete
find "$BACKUP_DIR" -name 'photos-*.tar.gz' -mtime +14 -delete

echo "backup: wrote $DB_OUT"