# Nextcloud on Cubeship

[Nextcloud](https://nextcloud.com) is the open-source file sync and share
platform: your files, calendars and contacts on your own server, reached from
the browser, the desktop and mobile clients, and WebDAV.

This template installs it on a Cubeship instance with the managed PostgreSQL
and Redis it needs, a volume for everything it keeps in files, and its
background jobs running every 5 minutes.

## What it creates

- **nextcloud** — Nextcloud `34.0.4`, built on the instance from the
  `Dockerfile` in this repository, answering on the domain you choose, with a
  volume at `/var/www/html`: Nextcloud itself, `config/config.php`, installed
  apps, and `data/` — every user's files.
- **nextcloud-db** — a managed PostgreSQL 18 database, attached to the app.
  Users, shares, calendars, contacts and the file index are in it.
- **nextcloud-redis** — a managed Redis 7.4, attached to the app. Nextcloud's
  file locks, its distributed cache and PHP sessions are in it.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

Nextcloud runs background jobs — cleaning up trash and versions, sending
notifications, scanning external storage — from `cron.php`, and recommends
system cron every 5 minutes. The published image, `nextcloud:34.0.4-apache`,
runs Apache and nothing else; with docker compose the image's `/cron.sh` runs
in a second container sharing the same volume. Cubeship has no sidecars and no
cron, and a volume belongs to one app, so that second container cannot exist.

The `Dockerfile` here is instead the image's own example for this case,
`.examples/dockerfiles/cron` in
[nextcloud/docker](https://github.com/nextcloud/docker): the same image with
`supervisord` running Apache and `/cron.sh` side by side, and
`NEXTCLOUD_UPDATE=1`, without which the entrypoint installs nothing unless the
command is Apache. The first time cron runs `cron.php`, Nextcloud switches its
background jobs to *Cron* by itself.

The alternatives Nextcloud offers are worse here: *AJAX* runs one job per page
view and its documentation calls it the least reliable; *Webcron* runs one job
per call from an outside service, which it calls fit only for very small
instances.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Nextcloud answers | A domain you control, pointed at your instance. |
| The admin's username | Anything; `admin` unless you change it. |
| The admin's password | Nothing — the instance generates it and shows it once. **Keep a copy.** |

Nextcloud installs itself on the first start, with that account, before Apache
answers, so there is no install wizard for a stranger to reach first.

## After installing

1. Wait for the first start to finish: it copies Nextcloud into the volume and
   creates the database tables, which takes a minute or two. The domain answers
   once it is done.
2. Open the domain and sign in with the admin's username and the generated
   password.
3. Open *Administration settings → Overview*. It lists what is left, usually:
   - **Email server** — set one under *Administration settings → Basic
     settings*. Until you do, password resets and share notifications go
     nowhere.
   - **Default phone region** and **maintenance window** — set both with `occ`,
     below.

## Running occ

Cubeship has no console into an app, so `occ` runs over SSH on the machine the
app is on, as `www-data`:

```bash
docker exec -u www-data $(docker ps -qf name=cubeship-nextcloud-production-nextcloud) \
  php occ status
```

The name follows the project, environment and app names you install it with.
The two commands the overview usually asks for:

```bash
php occ config:system:set default_phone_region --value=US
php occ config:system:set maintenance_window_start --type=integer --value=1
```

## Behind Cubeship's proxy

TLS ends at Cubeship's proxy, and Nextcloud gets plain HTTP. The app's
variables pin every address it builds to `https://<your domain>`:
`OVERWRITEPROTOCOL`, `OVERWRITEHOST` and `OVERWRITECLIURL`.

`TRUSTED_PROXIES` is the three private ranges Docker takes addresses from,
because Cubeship does not fix the subnet of the network the proxy reaches the
app over. It is what lets Nextcloud read the visitor's address from
`X-Forwarded-For`. The cost: any other container on the instance could claim a
different address in that header, which affects only Nextcloud's logs and its
brute-force throttling. The image's Apache already trusts the same ranges for
`X-Real-IP`.

`NEXTCLOUD_TRUSTED_DOMAINS` is read only on the first start. **If the domain
changes**, change `OVERWRITEHOST` and `OVERWRITECLIURL` on the app, then add
the new name with `occ` before redeploying, or Nextcloud refuses it:

```bash
php occ config:system:set trusted_domains 2 --value=cloud.example.org
```

## The health check

The health check asks for `/core/img/favicon.ico`, a file Apache serves
without Nextcloud. The proxy's probe does not carry your domain as its host,
and Nextcloud answers `400` to every host that is not trusted —
`/status.php` included.

## Uploads

PHP accepts up to 512 MB per request (`PHP_UPLOAD_LIMIT`) and a script may use
512 MB of memory (`PHP_MEMORY_LIMIT`). The web interface and the desktop and
mobile clients upload large files in chunks, so the limit rarely matters; set
either variable on the app and redeploy to change it.

## Updating Nextcloud

Nextcloud's web updater is disabled in this image. Change the tag in the
`Dockerfile`, release this repository, and point `ref` at the new release. On
the next start the entrypoint copies the new version into the volume and runs
`occ upgrade`.

Nextcloud upgrades **one major version at a time**: from 34 to 35, then to 36,
never 34 to 36 — the entrypoint refuses to start otherwise. Back up the volume
and the database first.

## The database password

Nextcloud's installer, given a PostgreSQL user that may create roles, creates
its own user, `oc_admin`, with a password it generates, and writes both into
`config/config.php` in the volume. Nextcloud connects as that user from then
on, so the database's own credentials no longer need to match what the app was
installed with.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Back up both the volume, for the files and the
configuration, and the database, for everything that indexes them: one without
the other does not restore.

Every user's files are in the volume. Size the disk of the machine the app is
on for them.

## Resources

The app is limited to 2 CPUs and 2 GiB of memory, which is Apache, PHP and
cron together. Raise `limits` in `template.yaml` for many users, previews of
large images, or apps like Nextcloud Office.
