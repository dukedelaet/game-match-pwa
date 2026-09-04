# Deploy GameMatch to Hostinger

Production is a push to the **`release`** branch. `main` is integration. Do not develop on `release`.

Full design: `docs/hostinger-business-deploy.md`.

## GitHub secrets and variables

**Secrets**

- `HOSTINGER_SSH_HOST`
- `HOSTINGER_SSH_USER`
- `HOSTINGER_SSH_KEY` (ed25519 private key; public half in hPanel)
- `HOSTINGER_SSH_KNOWN_HOSTS` (`ssh-keyscan -p 65002 HOST`)

**Variables**

- `HOSTINGER_SSH_PORT` = `65002`
- `HOSTINGER_DOMAIN_PATH` = `/home/u…/domains/<host>` (no trailing slash)
- `HOSTINGER_PHP_BIN` = `/opt/alt/php83/usr/bin/php`
- `PRODUCTION_URL` = `https://<host>`

Never put `APP_KEY` or the DB password in GitHub. Those live in `shared/.env` on the host.

## First-time host

1. PHP **8.3**, enable `gd`, remove `symlink` from `disable_functions`. Confirm `ln -sfn` over SSH.
2. Create `releases/` and `shared/storage/...` including `shared/storage/app/photos`.
3. Write `shared/.env` (`APP_ENV=production`, `APP_DEBUG=false`, `APP_URL=https://<host>`, MySQL **3306**, `SESSION_DRIVER=database`, `QUEUE_CONNECTION=database`, `CACHE_STORE=database`, `SESSION_SECURE_COOKIE=true`, `MAIL_MAILER=log`).
4. Optional: `mv public_html public_html.bak` before the first `release` push. If you skip it, `scripts/deploy-remote.sh` moves a stock Hostinger directory aside and symlinks `public_html` → `current/public`.
5. If the panel refuses a symlink on `public_html`, use the stub `scripts/hostinger-public-html-index.php` (`require __DIR__.'/../current/public/index.php'`) and rsync assets only.

DirectoryIndex should prefer `index.php` so `/` hits Laravel, which serves the SPA shell. `/index.html` may bypass Laravel; that is usually fine.

## Cron

After `current` exists:

```
* * * * * /opt/alt/php83/usr/bin/php /home/u…/domains/<host>/current/artisan schedule:run >> /home/u…/domains/<host>/shared/storage/logs/cron.log 2>&1
```

Use php83, not the plan-default `php` if that is 8.2. This advances live games and drains the database queue (`queue:work --stop-when-empty`) — there is no daemon.

## First seed (once)

```
$PHP artisan db:seed --class=CatalogSeeder --force
$PHP artisan gamematch:make-admin +E164
```

Never `DatabaseSeeder` / `migrate:fresh` on Hostinger.

Closed-beta OTP: `POST /v1/auth/otp/start`, then `$PHP artisan gamematch:otp +E164`.

## Rollback

```
cd /home/u…/domains/<host>
ls releases/
ln -sfn releases/<PREVIOUS_SHA> current
# if public_html is a symlink:
ln -sfn current/public public_html
```

Or revert the merge on `release` and push.

Logs: `tail -f shared/storage/logs/laravel-YYYY-MM-DD.log`

If `/up` 500s right after a flip, wait a few seconds or `touch current/public/index.php` (OPcache).
