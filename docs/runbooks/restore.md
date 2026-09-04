# Restore from backup

Hostinger Business includes daily MySQL backups.

1. Download the latest dump from hPanel.
2. `mysql -u … -p … < dump.sql` (use the hPanel database name, often prefixed).
3. Confirm `php artisan migrate:status` is current on `current/`.
4. Photo files live in **`shared/storage/app/photos`** — restore those from the account file backup.

If catalogs were wiped, re-run `php artisan db:seed --class=CatalogSeeder --force` (`updateOrCreate`, safe on non-empty tables).

Never `migrate:fresh` and never `DatabaseSeeder` on Hostinger.

Locally, `php artisan migrate:fresh --seed` rebuilds a demo database (destroys data; includes Alex/Jordan).

