# Odoo on Cubeship

[Odoo](https://www.odoo.com) is an open-source suite of business apps: CRM,
sales, invoicing and accounting, inventory, projects, a website builder and
more, all sharing one database.

This template installs Odoo Community on a Cubeship instance, with the managed
Postgres it keeps everything in and a volume for the files it stores beside it.

## What it creates

- **odoo** — Odoo Community 19.0, built on the instance from the `Dockerfile`
  in this repository on top of `odoo:19.0-20260908`. It answers on the domain
  you choose and keeps attachments and sessions in a volume at
  `/var/lib/odoo`.
- **odoo-db** — a managed Postgres 17, with a login called `odoo` and a
  database called `odoo`.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `odoo`, starts with no database. On a first visit it
opens its database manager, a public page where whoever arrives first creates
one. A database made there would also not be the one Cubeship backs up:
Cubeship dumps the database it created, `odoo`, and Odoo would make another.

So the `Dockerfile` here is that image with two additions:

- [`odoo.conf`](odoo.conf): proxy mode on, the threaded server, and the
  database manager off.
- [`cubeship-entrypoint.sh`](cubeship-entrypoint.sh), which runs before the
  image's own entrypoint. At every start it writes the master password into
  `odoo.conf` — Odoo reads it from that file and nowhere else. On the first
  start, while the `odoo` database is still empty, it creates it with
  `odoo db init`: your language and country, your login and password, and no
  demo data. It never replaces a database that has tables in it.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Odoo answers | A domain you control, pointed at your instance. |
| The email you sign in with | Your login as Odoo's administrator. It also becomes the company's email address. |
| The password you sign in with | Nothing — the instance generates it and shows it once. It is used when the database is created, and never again: change it in Odoo afterwards, not in the app's settings. |
| Odoo's master password | Nothing — generated and shown once. It guards creating, copying, dumping and deleting databases. With the database manager off nothing asks for it, but keep it for the day you turn the manager on. |
| The language the database is created in | An Odoo language code: `en_US`, `fr_FR`, `pt_BR`, `es_419`… More languages can be installed later. |
| The company's country | Optional. A two-letter code, like `US` or `BR`. It sets the company's currency, and everyone's timezone when the country has only one. Leave it empty to set it later. |

A language or country Odoo does not know makes the first start fail: the
database is dropped again and the app keeps restarting. Correct the variable
in the app's settings and deploy.

## After installing

1. **Wait a few minutes.** The first start creates the database and installs
   Odoo's base modules, and the domain answers `503` until that is done. The
   app's logs say `Creating Odoo database odoo` when it starts, and Odoo's own
   log follows.
2. Open the domain and sign in with the email and generated password.
3. Change the password under your avatar → *My Preferences → Security*.
4. Open *Apps* and install what you need. Set the company's country under
   *Settings → Companies* **before installing Invoicing or Accounting**: the
   chart of accounts and taxes are picked from it.
5. Set up an outgoing mail server in *Settings*. Odoo sends no email until you
   do.

Cubeship has no console into an app. Anything that needs a shell is done over
SSH on the machine the app runs on, with `docker exec`. To reset the
administrator's password, for instance:

```bash
docker exec -it $(docker ps -qf name=cubeship-odoo-production-odoo) \
  odoo shell --no-http
```

```python
env['res.users'].browse(2).password = 'a new password'
env.cr.commit()
```

The container name follows the project, environment and app names you install
with. `browse(2)` is the administrator the database was created with.

## Choices this template makes

- **The threaded server, not workers.** Odoo's live features — chat,
  notifications, a record updating while another person edits it — go over a
  websocket at `/websocket`. With `workers` set, Odoo serves that on a second
  port, `8072`, and expects a proxy to send `/websocket` there. A Cubeship
  domain reaches one port. The threaded server (`workers = 0`) serves the
  websocket on `8069` with everything else, so it all works on one domain.

  The cost is the one Odoo's deployment guide describes: one Python process,
  so heavy requests share one CPU core, and no per-worker memory or time
  limits recycling a runaway request. It suits a company of a few dozen people.
  Beyond that, Odoo's multi-process server wants a proxy that Cubeship cannot
  configure for one app.
- **No database manager.** `list_db = False` turns off the page at
  `/web/database/manager` and the database calls of the XML-RPC API, as Odoo's
  guide advises once an instance serves one database. The database is named by
  `PGDATABASE`, and Odoo serves only that one.
- **Proxy mode on.** TLS ends at Cubeship's proxy, and Odoo needs
  `proxy_mode = True` to believe the `X-Forwarded-*` headers — otherwise its
  links are `http://` and every login is logged from the proxy's address.
- **No demo data.** To try Odoo with sample records, install a separate copy;
  demo data cannot be removed from a database once loaded.
- **The database login is a superuser.** Cubeship's managed Postgres gives its
  one login every privilege, and Odoo's guide says its login should not be a
  superuser. Odoo only refuses the name `postgres`, so it runs. The Postgres
  server is Odoo's alone.

## Backups

Odoo keeps every record in the database, and the files attached to them —
attachments, images, the compiled web assets — in the volume, under
`/var/lib/odoo/filestore/odoo`. **A backup needs both**, taken close together:
a database restored without its filestore shows broken images and missing
attachments.

- Back the database up from **odoo-db**'s page in the dashboard.
- Back the volume up from the app's settings.

Restore both, then deploy the app.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds: sessions survive, open pages reconnect. Custom modules
are not in the volume — `/mnt/extra-addons` is empty and disappears with each
container. To add modules, fork this repository and `COPY` them into
`/mnt/extra-addons` in the `Dockerfile`.

## Updating

Odoo publishes a dated image for every nightly build of 19.0. Change the tag in
the `Dockerfile`, release this repository — or your fork — and point the app's
ref at the new release. Back up the database and the volume first. After a
deploy, update the installed modules if the release notes ask for it:

```bash
docker exec -it $(docker ps -qf name=cubeship-odoo-production-odoo) \
  odoo -u all --stop-after-init --no-http
```

Moving to a new major version (20.0) is a migration, not a tag change: Odoo
Community has no upgrade path of its own; [OpenUpgrade](https://github.com/OCA/OpenUpgrade)
is the community's.

## Resources

The app is limited to 2 CPUs and 2 GiB of memory; the threaded server uses one
core for Python work at a time. Raise `limits` in `template.yaml` for many
users or large imports.
