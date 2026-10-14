# Restore from backup

> **Read the Docker procedure first.** GameMatch runs under Docker Compose; the
> `docker compose` path below is the current one and the only one the backup
> container is built for. The systemd procedure is kept at the end for hosts that
> still run the legacy units (see [`deploy.md`](deploy.md)). The *reasoning* is
> identical for both — only the paths, the privileges and the command that stops
> the server differ — so the argument is written once, here, above both.

The database is a single SQLite file at `DATA_DIR/gamematch.db` and uploaded
photos live beside it in `DATA_DIR/photos`. Under Compose those are `./data` and
`./backups` inside the checkout.

## Where the backups come from, and how often

`scripts/backup.sh` writes two artefacts per run, `gamematch-<stamp>.db` and
`photos-<stamp>.tar.gz`, and prunes anything older than 14 days. **How often it
runs depends on the deployment:**

| Deployment | Interval | Recovery point objective | Retention |
| --- | --- | --- | --- |
| **Docker Compose (current)** — the `backup` service in `docker-compose.yml` | **every 6 hours** (`sleep 21600`, plus the duration of the run itself), and once shortly after every start | **up to 6 hours** of lost writes | 14 days, ~56 snapshots |
| systemd (legacy) — `gamematch-backup.timer` | hourly | 1 hour | 14 days |

**The compose stack's RPO is up to six hours, not one.** If you are relying on
this runbook for a production recovery, the newest snapshot you can possibly
restore is up to six hours old. The `backup` container also reports `unhealthy`
when the newest `.db` is more than 7 hours old — that is the signal that a whole
interval was missed:

```bash
docker compose ps backup
docker compose logs --tail 5 backup
```

Do not be misled by `Up (healthy)` alone. The loop is built to survive a failed
iteration, so a run that fails every six hours leaves the container `Up` forever
with nothing to show for it. **The check that cannot be misread is the
directory:**

```bash
ls -l backups/
```

One `.db`, and — once `data/photos` exists — a `photos-*.tar.gz` with the same
stamp. **A missing `photos-*.tar.gz` is a real problem, not a cosmetic one.**
`scripts/backup.sh` guards the archive with `if [ -d "$DATA_DIR/photos" ]` and
otherwise does nothing at all: no warning, exit 0, a valid `.db` written as
usual, and the container stays `healthy` because its healthcheck only looks at
`.db` files. If the photos directory has been deleted, restored from the wrong
place, or left unmounted by a bind-mount typo, **every subsequent backup is
silently database-only, every six hours, indefinitely.** The database keeps its
photo rows, so nothing in the app breaks and the loss only surfaces here, at
recovery time. Check `ls -ld data/photos` whenever a backup listing looks short.

Backups live on the same disk as the database. Copy them off-box or a disk
failure takes both.

## Do not copy the database file

The service runs SQLite in WAL mode, so committed transactions can live in the
`gamematch.db-wal` sidecar until they are checkpointed. Copying only the `.db`
against a running server produces a file that is missing whatever has not been
checkpointed — in practice including the schema itself. This was verified
against a live database: a plain `cp` of the `.db` yielded `no such table`,
while `sqlite3 .backup` taken from the same database captured the canary row
and the schema.

The backups written by `scripts/backup.sh` are already consistent, because it
uses `sqlite3 .backup` against the live database.

The corollary, and the reason step 1 of either procedure below is not optional:
**a `-wal` left beside a freshly restored database is replayed onto it and
corrupts the result.** Removing the sidecars is required, not optional.

---

## Procedure — Docker Compose

1. **Stop the API.** Nothing may be mid-write, and the running process will
   recreate the `-shm`/`-wal` sidecars underneath you.

   ```bash
   docker compose stop api
   ```

   SQLite checkpoints and deletes both sidecars on a clean close, which is what
   makes the next step possible:

   ```
   $ ls data
   gamematch.db
   photos
   ```

   (No `gamematch.db-wal`, no `gamematch.db-shm`.)

2. **Pick the snapshot and check it before trusting it.** A snapshot that opens
   is not necessarily a good one — `scripts/backup.sh` verifies integrity and
   schema count itself, but confirming is cheap:

   ```bash
   STAMP=20261014T222905Z
   sqlite3 "backups/gamematch-$STAMP.db" 'PRAGMA integrity_check;'          # ok
   sqlite3 "backups/gamematch-$STAMP.db" 'SELECT count(*) FROM users;'      # what you expect
   ls -l "backups/photos-$STAMP.tar.gz"                                     # must exist if data/photos does
   ```

3. **Move the existing database aside** rather than deleting it, so a failed
   restore is recoverable, and delete the sidecars.

   ```bash
   sudo mv data/gamematch.db data/gamematch.db.pre-restore
   sudo rm -f data/gamematch.db-wal data/gamematch.db-shm
   ```

   These need privileges: `./data` is mode `0755` owned by UID 10001, so only
   that UID can create or rename an entry in it. As a non-root deploy user this
   is refused:

   ```
   $ mv data/gamematch.db data/gamematch.db.pre-restore
   mv: cannot move 'data/gamematch.db' to 'data/gamematch.db.pre-restore': Permission denied
   ```

4. **Restore the database, and the photos if there is an archive for it.**

   ```bash
   sudo cp "backups/gamematch-$STAMP.db" data/gamematch.db

   # The photos archive is GUARDED, and must be.
   #
   # `data/photos` does not exist on a fresh stack — only the photo upload
   # handler creates it — so a snapshot taken early legitimately has no
   # archive. Running `tar` unconditionally exits 2 on the missing file, and
   # it does so AFTER step 3 has already replaced your database. You end up
   # with a restored database and a non-zero exit code and no photos.
   #
   # The else branch is not just cosmetic: a missing archive on a stack that
   # DOES have photos means data/photos was gone when the snapshot was taken,
   # which is the silent database-only-backup failure. See "Where the
   # backups come from" above.
   if [ -f "backups/photos-$STAMP.tar.gz" ]; then
       # Move the live tree aside, or every photo uploaded after the
       # snapshot survives as an orphan no database row references.
       [ -d data/photos ] && sudo mv data/photos data/photos.pre-restore
       sudo tar xzf "backups/photos-$STAMP.tar.gz" -C data
   else
       echo "no photos archive for $STAMP"
       echo "  either the snapshot predates the first upload, or data/photos"
       echo "  was missing when it was taken -- check the backup listing"
   fi

   sudo chown -R 10001:10001 data
   ```

   `chown -R`, not `chown` on the `.db` alone: the extracted photos directory
   comes back root-owned otherwise and the upload handler cannot write to it.
   The API container runs as UID 10001 and nothing else.

5. **Start the API.** Migrations run automatically on boot (the app applies any
   embedded migration newer than the database's `PRAGMA user_version`), so there
   is no migration step to remember — and none to forget.

   ```bash
   docker compose start api
   ```

6. **Verify.**

   ```bash
   curl -sk -o /dev/null -w '%{http_code}\n' https://your.domain/v1/healthz    # 200
   docker inspect --format '{{.RestartCount}}' gamematch-api-1                # 0
   docker compose logs api | tail -5
   ```

   Then check the database itself. **Do this as root, or only after the API is
   back up** — `sqlite3` opens read-write by default, and with the API stopped
   there is no `-shm` sidecar for it to use, so a plain host-side query against
   `./data` fails:

   ```
   $ sqlite3 data/gamematch.db 'SELECT count(*) FROM users;'
   Parse error in 2nd command line argument: attempt to write a readonly database (8)
   $ ls -ln data
   -rw-r--r-- 1 10001 10001 516096 Oct 14 22:30 gamematch.db
   -rw-r--r-- 1 10001 10001 32768 Oct 14 15:31 gamematch.db-shm
   -rw-r--r-- 1 10001 10001     0 Oct 14 15:31 gamematch.db-wal
   ```

   The second listing is *after* `docker compose start api`: the API recreates
   both sidecars on open, and the same query then works. Either way:

   ```bash
   sqlite3 data/gamematch.db 'PRAGMA integrity_check;'   # ok
   sqlite3 data/gamematch.db 'SELECT count(*) FROM users;'
   sqlite3 data/gamematch.db 'PRAGMA user_version;'
   ```

   A last resort if the host cannot be trusted: copy the file somewhere readable
   and query it there. The `backup` container also works at any point, with the
   API up or down:

   ```bash
   docker compose exec backup sqlite3 /data/gamematch.db 'PRAGMA journal_mode;'
   ```

7. **Clear the aside files.** Until you do, `./data` is holding a **complete
   stale copy of the pre-restore database** plus the pre-restore photo tree,
   inside the directory the backup service archives from — and nothing prunes
   either.

   ```bash
   sudo rm -rf data/gamematch.db.pre-restore data/photos.pre-restore
   ```

   Do this only once the restore is verified. These files are the rollback, and
   the rollback is worth exactly as much as the time you keep them for.

8. **Take a fresh backup immediately**, so the restored state is protected again
   within one interval rather than six.

   ```bash
   docker compose exec backup /scripts/backup.sh /backups
   ```

   Run it through the **service's own entry point**. Overriding the entry point
   replaces the service's `command:` — which is where `apk add bash sqlite`
   lives — and the script's `#!/usr/bin/env bash` shebang then has nothing to
   run:

   ```
   $ docker compose run --rm --entrypoint /scripts/backup.sh backup /backups
   env: can't execute 'bash': No such file or directory
   rc=127
   ```

   (An `apk add` inside a `run --rm --entrypoint sh -c …` works too, and is the
   form to use when the `backup` container is not running.)

### Reading the result

A restore that reports the right row counts is a restore that copied a file. To
check it rolled back to the right point in time, compare against what the live
database held immediately before:

```
$ sqlite3 data/gamematch.db 'SELECT count(*) FROM users;'    # after the restore
5
$ diff /tmp/before.txt /tmp/after.txt && echo "restore verified"
restore verified
```

`/tmp/before.txt` was `SELECT count(*) FROM users;` against the **snapshot**,
taken before anything was moved. A user that exists in the live database but not
in the snapshot should be **absent** afterwards — that is the proof the restore
took effect rather than silently doing nothing.

---

## Procedure — systemd (legacy hosts)

Identical reasoning, different paths. See [`deploy.md`](deploy.md) for how a
legacy host is built.

**Every `mv`, `rm`, `cp` and `chown` below needs `sudo`,** exactly as on the
compose path. `DATA_DIR` is `/var/lib/gamematch`, created by
`sudo install -d -o gamematch -g gamematch` and not writable by the deploy user;
the `chown` calls only work as root in any case. This is the same ownership trap
that was corrected on the compose path, and it is worth not repeating here.

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

   sudo mv "$DATA_DIR/gamematch.db" "$DATA_DIR/gamematch.db.old"
   # A -wal left beside a freshly restored database is replayed onto it and
   # corrupts the result. Removing the sidecars is required, not optional.
   sudo rm -f "$DATA_DIR/gamematch.db-wal" "$DATA_DIR/gamematch.db-shm"
   ```

3. Restore the database:

   ```bash
   sudo cp "$BACKUP_DIR/gamematch-$STAMP.db" "$DATA_DIR/gamematch.db"
   sudo chown gamematch:gamematch "$DATA_DIR/gamematch.db"
   ```

4. Restore photos — guarded, for the same reason as on the compose path:

   ```bash
   if [ -f "$BACKUP_DIR/photos-$STAMP.tar.gz" ]; then
       [ -d "$DATA_DIR/photos" ] && sudo mv "$DATA_DIR/photos" "$DATA_DIR/photos.old"
       sudo tar xzf "$BACKUP_DIR/photos-$STAMP.tar.gz" -C "$DATA_DIR"
   else
       echo "no photos archive for $STAMP"
   fi
   sudo chown -R gamematch:gamematch "$DATA_DIR/photos"
   ```

5. Start the server. Migrations run automatically on boot (the app applies any
   embedded migration newer than the database's `PRAGMA user_version`).

   ```bash
   sudo systemctl start gamematch
   ```

6. Verify. A restored database that opens is not necessarily the right one:

   ```bash
   sudo sqlite3 "$DATA_DIR/gamematch.db" 'PRAGMA integrity_check;'   # ok
   sudo sqlite3 "$DATA_DIR/gamematch.db" 'SELECT count(*) FROM users;'
   sudo sqlite3 "$DATA_DIR/gamematch.db" 'PRAGMA user_version;'
   curl -fsS http://127.0.0.1:8000/v1/healthz
   ```

7. Clear the aside files once verified:

   ```bash
   sudo rm -rf "$DATA_DIR/gamematch.db.old" "$DATA_DIR/photos.old"
   ```

---

## Rebuilding a demo database from scratch

Locally, deleting the database file and running `go run ./cmd/gamematch seed`
(or `docker compose run --rm api seed`) rebuilds a fresh demo database. This
**destroys data** and is a development convenience, not a recovery step.

## Verifying the runbook itself

A restore procedure that has never been executed is a guess. `deploy-docker.md`
[Appendix B.3](deploy-docker.md#b3--the-restore-drill) records a full drill run
end to end against a live stack on 2026-10-14 — seed, sign in, upload a photo,
let the scheduled backup land, write one more user, then restore and diff the
counts — including the two commands in the Compose procedure above that fail as
written for a non-root deploy user. Re-run it after any change to
`scripts/backup.sh`, `docker-compose.yml` or the schema.