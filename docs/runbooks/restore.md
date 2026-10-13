# Restore from backup

The database is a single SQLite file at `DATA_DIR/gamematch.db` (default
`apps/server/data/gamematch.db`), and uploaded photos live beside it in
`DATA_DIR/photos`.

Backups are produced by `scripts/backup.sh`, which runs hourly from the
`gamematch-backup.timer` unit. Each run writes `gamematch-<stamp>.db` and
`photos-<stamp>.tar.gz` into the backup directory, keeping 14 days.

## Do not copy the database file

The service runs SQLite in WAL mode, so committed transactions can live in the
`gamematch.db-wal` sidecar until they are checkpointed. Copying only the `.db`
against a running server produces a file that is missing whatever has not been
checkpointed -- in practice including the schema itself. This was verified
against a live database: a plain `cp` of the `.db` yielded `no such table`,
while `sqlite3 .backup` taken from the same database captured the canary row
and the schema.

The backups written by `scripts/backup.sh` are already consistent, because it
uses `sqlite3 .backup` against the live database.

## Procedure

1. Stop the server so nothing is mid-write.

   ```bash
   sudo systemctl stop gamematch
   ```

2. Move the existing database aside rather than deleting it, so a failed restore
   is recoverable:

   ```bash
   DATA_DIR=/var/lib/gamematch
   BACKUP_DIR=/backup/gamematch
   STAMP=20261012T230000Z

   mv "$DATA_DIR/gamematch.db" "$DATA_DIR/gamematch.db.old"
   # A -wal left beside a freshly restored database is replayed onto it and
   # corrupts the result. Removing the sidecars is required, not optional.
   rm -f "$DATA_DIR/gamematch.db-wal" "$DATA_DIR/gamematch.db-shm"
   ```

3. Restore the database:

   ```bash
   cp "$BACKUP_DIR/gamematch-$STAMP.db" "$DATA_DIR/gamematch.db"
   chown gamematch:gamematch "$DATA_DIR/gamematch.db"
   ```

4. Restore photos:

   ```bash
   tar xzf "$BACKUP_DIR/photos-$STAMP.tar.gz" -C "$DATA_DIR"
   chown -R gamematch:gamematch "$DATA_DIR/photos"
   ```

5. Start the server. Migrations run automatically on boot (the app applies any
   embedded migration newer than the database's `PRAGMA user_version`).

   ```bash
   sudo systemctl start gamematch
   ```

6. Verify. A restored database that opens is not necessarily the right one:

   ```bash
   sqlite3 "$DATA_DIR/gamematch.db" 'PRAGMA integrity_check;'   # ok
   sqlite3 "$DATA_DIR/gamematch.db" 'SELECT count(*) FROM users;'
   sqlite3 "$DATA_DIR/gamematch.db" 'PRAGMA user_version;'
   curl -fsS http://127.0.0.1:8000/v1/healthz
   ```

## Recovery objectives

The timer runs hourly, so the recovery point objective is one hour of lost
writes and the recovery time is a stop, two copies and a start.

Locally, deleting the database file and running
`go run ./cmd/gamematch seed` rebuilds a fresh demo database (destroys data).