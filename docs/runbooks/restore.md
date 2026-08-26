# Restore from backup

Hostinger Business includes daily MySQL backups.

1. Download the latest dump from hPanel.
2. `mysql -u gamematch -p gamematch < dump.sql`
3. Confirm `php artisan migrate:status` is current.
4. Photo files live in `storage/app/photos` — restore those from the account file backup.

Locally, `php artisan migrate:fresh --seed` rebuilds a demo database (destroys data).
