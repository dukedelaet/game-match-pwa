# Restore from backup

The database is a single SQLite file at `DATA_DIR/gamematch.db` (default
`apps/server/data/gamematch.db`), and uploaded photos live beside it in
`DATA_DIR/photos`.

1. Stop the server so nothing is mid-write.
2. Restore the file backup, e.g. `cp gamematch.db.bak "$DATA_DIR/gamematch.db"`.
3. Restore the photo directory from the same backup.
4. Start the server. Migrations run automatically on boot (the app applies any
   embedded migration newer than the database's `PRAGMA user_version`).
5. Confirm with `sqlite3 "$DATA_DIR/gamematch.db" 'PRAGMA user_version;'`.

Locally, deleting the database file and running
`go run ./cmd/gamematch seed` rebuilds a fresh demo database (destroys data).
