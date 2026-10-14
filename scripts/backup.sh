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

# Build every artefact under a hidden subdirectory and move it into place only
# once it has been verified.
#
# This is not tidiness, it is the difference between a snapshot and a file that
# looks like one. A backup interrupted part-way through the write -- container
# SIGKILL, power loss, the disk filling up -- otherwise leaves the result at its
# FINAL name, and the checks below never run because they are a separate
# process after a write that has already been killed.
#
# What survives that way is not recognisably broken. A truncated SQLite file is
# still a structurally valid database: verified on alpine:3.24, a snapshot
# killed mid-write answered "PRAGMA integrity_check" with ok and
# "SELECT count(*) FROM sqlite_master" with 0, which is exactly what the
# restore runbook tells an operator to check. Worse, the first reader to open
# such a file rolls back its hot journal and truncates it to 0 bytes, after
# which it is a valid EMPTY database under a name indistinguishable from a good
# snapshot. It would then sit in the backup directory for the full 14-day
# prune window looking exactly like the backup it is not.
#
# Writing to a temporary name and renaming means the final path only ever holds
# a file that passed every check. mv within one filesystem is rename(2), which
# is atomic, so a snapshot appears whole or not at all.
TMP_DIR="$BACKUP_DIR/.tmp"
mkdir -p "$TMP_DIR"
# Clear debris an interrupted run left here. A completed run moves its only
# file out, so anything still present is from a run that died. Concurrent runs
# against one BACKUP_DIR remain unsupported, as they already were: two runs
# within the same second share a STAMP and collide either way.
rm -f "$TMP_DIR"/*

DB_TMP="$TMP_DIR/gamematch-$STAMP.db"

# Online backup: consistent without stopping the service or locking writers.
sqlite3 "$DATA_DIR/gamematch.db" ".backup '$DB_TMP'"

# Verify before trusting. A truncated or unreadable backup is worse than none,
# because it is found at restore time. Messages name $DB_OUT, not $DB_TMP,
# because that is the name an operator goes looking for.
if ! sqlite3 "$DB_TMP" 'PRAGMA integrity_check;' | grep -qx ok; then
	echo "backup: integrity check failed for $DB_OUT" >&2
	rm -f "$DB_TMP"
	exit 1
fi
if ! sqlite3 "$DB_TMP" 'SELECT count(*) FROM sqlite_master;' | grep -qE '^[1-9]'; then
	echo "backup: snapshot has no schema" >&2
	rm -f "$DB_TMP"
	exit 1
fi

mv "$DB_TMP" "$DB_OUT"

if [ -d "$DATA_DIR/photos" ]; then
	# Same reasoning as the database: a .tar.gz is never inspected by anything
	# after it is written, so a half-written one fails at restore time instead.
	# gzip -t reads the stream back, which is what catches that here -- tar
	# alone cannot, because a truncated archive is still a readable gzip
	# prefix.
	PHOTOS_TMP="$TMP_DIR/photos-$STAMP.tar.gz"
	tar czf "$PHOTOS_TMP" -C "$DATA_DIR" photos
	if ! gzip -t "$PHOTOS_TMP" 2>/dev/null; then
		echo "backup: photos archive failed its check for $PHOTOS_OUT" >&2
		rm -f "$PHOTOS_TMP"
		exit 1
	fi
	mv "$PHOTOS_TMP" "$PHOTOS_OUT"
fi

# Keep 14 days. Runs hourly, so this is two weeks of history.
find "$BACKUP_DIR" -name 'gamematch-*.db' -mtime +14 -delete
find "$BACKUP_DIR" -name 'photos-*.tar.gz' -mtime +14 -delete

# Leave no empty scratch directory behind. Fails harmlessly if an interrupted
# run left debris, which the next run sweeps.
rmdir "$TMP_DIR" 2>/dev/null || true

echo "backup: wrote $DB_OUT"