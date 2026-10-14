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

# Build every artefact under a private scratch directory, and move it into place
# only after it has been verified and flushed to disk.
#
# This is the difference between a snapshot and a file that looks like one. A
# backup interrupted part-way through the write -- container SIGKILL, the OOM
# killer, the disk filling up -- otherwise leaves the result at its FINAL name,
# and the checks never run because they are a separate process after a write
# that has already been killed.
#
# What survives that way is not recognisably broken. A truncated SQLite file is
# still a structurally valid database: verified on alpine:3.24, a snapshot
# killed mid-write answered "PRAGMA integrity_check" with ok and
# "SELECT count(*) FROM sqlite_master" with 0, which is exactly what the
# restore runbook tells an operator to check. Worse, the first reader to open
# such a file rolls back its hot journal and truncates it to 0 bytes, after
# which it is a valid EMPTY database under a name indistinguishable from a good
# snapshot, and it would sit there for the full 14-day prune window.
#
# Each artefact therefore appears at its final path whole or not at all. The
# SET is not atomic: a crash between the two renames publishes a database whose
# photos archive has not landed yet. Making the pair atomic needs a manifest or
# a single directory rename, which is more than this script promises.

# mktemp -d, not a fixed name. Two backups CAN overlap -- `docker compose run
# --rm backup` alongside the scheduled loop, or an `up -d` that recreates the
# container mid-run -- and with a shared scratch directory whichever starts
# second deletes the first's in-flight file. Verified on a 1.4G database with
# two runs 5s apart: the second run's cleanup killed the first with "Error:
# disk I/O error", rc=1. The systemd timer cannot hit this (Type=oneshot never
# overlaps), but the container has no such guarantee.
# A unique directory also makes leftover debris attributable to the run that
# left it. It sits inside $BACKUP_DIR, so it is on the same filesystem and mv
# remains rename(2).
TMP_DIR="$(mktemp -d "$BACKUP_DIR/.tmp.XXXXXX")"
# mktemp -d creates 0700. Back to 0755: the scratch holds nothing secret (it
# becomes a snapshot in ./backups, which is world-readable anyway), and debris
# that nobody can list cannot be attributed to the run that left it, which is
# the other half of using a unique name.
chmod 755 "$TMP_DIR"
# Discard this run's scratch on every normal exit path, including the guarded
# failures below. This cannot catch an uncaught fatal signal (SIGKILL, and
# SIGTERM/SIGINT arriving while a foreground child runs -- bash skips an EXIT
# trap in that case, which is what `docker compose stop` mid-backup produces),
# so the sweep below is what bounds that debris.
trap 'rm -rf "$TMP_DIR"' EXIT

# Remove scratch left by runs that were killed outright, including the fixed
# .tmp directory used before mktemp.
#
# Bounded by AGE rather than by "whatever is there", because an overlapping
# run's scratch is younger than 24h and must survive. This runs before the
# snapshot is written rather than at the end, because the retention window
# below only executes at the end of a fully successful run: if backups kept
# failing, debris from an earlier kill would otherwise sit forever.
find "$BACKUP_DIR" -maxdepth 1 -name '.tmp*' -type d -mmin +1440 -exec rm -rf {} +

DB_TMP="$TMP_DIR/gamematch-$STAMP.db"

# Online backup: consistent without stopping the service or locking writers.
sqlite3 "$DATA_DIR/gamematch.db" ".backup '$DB_TMP'"

# Verify before trusting. A truncated or unreadable backup is worse than none,
# because it is found at restore time. Messages name $DB_OUT, not $DB_TMP,
# because that is the name an operator goes looking for.
if ! sqlite3 "$DB_TMP" 'PRAGMA integrity_check;' | grep -qx ok; then
	echo "backup: integrity check failed for $DB_OUT" >&2
	exit 1
fi
if ! sqlite3 "$DB_TMP" 'SELECT count(*) FROM sqlite_master;' | grep -qE '^[1-9]'; then
	echo "backup: snapshot has no schema" >&2
	exit 1
fi

# rename(2) makes the directory entry atomic, but only for bytes the kernel has
# already written. The page cache outlives a SIGKILL, which is the case all of
# the above is about, but a host that loses power after the rename can still
# surface the garbage file at the final name -- the same directory, for one
# fsync every six hours. `sync FILE` is supported by both busybox and coreutils.
sync "$DB_TMP"

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
		exit 1
	fi
	sync "$PHOTOS_TMP"
	mv "$PHOTOS_TMP" "$PHOTOS_OUT"
fi

# Keep 14 days. The interval is the caller's, not this script's: hourly from
# gamematch-backup.timer, every 6 hours from the compose backup service. At
# 6-hourly that is ~56 snapshots.
find "$BACKUP_DIR" -name 'gamematch-*.db' -mtime +14 -delete
find "$BACKUP_DIR" -name 'photos-*.tar.gz' -mtime +14 -delete

# Sweep sidecars whose snapshot is gone.
#
# Reading a snapshot with sqlite3 leaves gamematch-<stamp>.db-shm (32K) and
# .db-wal (0) beside it -- even with mode=ro, because a WAL database's index
# lives in the shared-memory file. The two finds above match neither name, so
# before this sweep those accumulated for the life of a deployment: ~46MB/year
# across four runs a day, unbounded.
#
# The rule is ORPHANED, not OLD. A sidecar whose .db is still present belongs
# to a snapshot that exists and may be being read right now, and it survives
# however old it is -- age is the retention policy's business, already
# applied above. Only once the .db itself is gone is the sidecar meaningless:
# SQLite never creates a -shm without its database, so its absence is proof.
#
# After the two finds above rather than before, so a snapshot this run prunes
# by age takes its sidecars with it in the same run.
#
# The globs do not cross '/', so this never reaches a concurrent run's
# $TMP_DIR -- and it should not: that snapshot is mid-write and its sidecars
# are removed by that run's own trap.
for sidecar in "$BACKUP_DIR"/gamematch-*.db-shm "$BACKUP_DIR"/gamematch-*.db-wal; do
	[ -e "$sidecar" ] || continue
	case "$sidecar" in
	*.db-shm) snapshot="${sidecar%.db-shm}.db" ;;
	*.db-wal) snapshot="${sidecar%.db-wal}.db" ;;
	esac
	if [ ! -e "$snapshot" ]; then
		rm -f "$sidecar"
	fi
done

echo "backup: wrote $DB_OUT"