# Immich on Cubeship

[Immich](https://immich.app) is a self-hosted photo and video backup: phone
apps that upload in the background, a web timeline, albums and sharing, and
search by faces, places and what is in the picture. This template installs it
on a Cubeship instance.

## What it creates

- **immich** — the Immich server `v3.2.0`: the web app, the API the phone apps
  talk to, and the background jobs that make thumbnails and transcode videos.
  It answers on the domain you choose and keeps every upload in a volume at
  `/data`.
- **machine-learning** — Immich's machine learning `v3.2.0`, on the CPU: smart
  search, face detection and recognition, and text in images. It has no
  domain, listens on port `3003` inside the instance, and keeps the models it
  downloads in a volume at `/cache`.
- **immich-db** — a managed Postgres 17 with the `pgvector` and `vectorchord`
  extensions, where Immich keeps everything but the files themselves.
- **immich-redis** — a managed Redis 7.4, where Immich queues its jobs.

Machine learning is deployed first, then the server.

It needs Cubeship 0.9.0 or newer, and **an admin to install it**: creating a
managed database is an admin's.

## The database

Immich stores its search and face embeddings as `pgvector` columns and indexes
them with VectorChord. Both are extensions Cubeship can create a managed
Postgres with, so there is no database container to build here and no volume to
back up separately — it is a database the instance runs, backs up and charts
like any other.

That is a change from earlier releases of this template, which ran Immich's own
Postgres image as an app. **An existing installation is not migrated.** Its
`postgres` app and the volume under it stay exactly where they are; moving to
the managed database means installing this template fresh and moving the data
across with `pg_dump` and `psql`.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Immich answers | A domain you control, pointed at your instance. The phone apps connect to it. |

The database password is not asked for: the instance generates one and hands it
to Immich. Read it from the database's page if you ever need it.

## After installing

1. **Open `https://<your domain>` straight away.** Immich has no default
   account: the first person to sign up becomes the admin.
2. In the Immich app on your phone, enter `https://<your domain>` as the server
   URL and sign in.

Immich finds the machine learning app through `IMMICH_MACHINE_LEARNING_URL` on
the `immich` app. That is only the default of the setting under
Administration → Settings → Machine Learning: a URL saved there wins.

The first time a job needs a model, machine learning downloads it from Hugging
Face, so the server needs to reach the internet. Until then smart search and
faces show nothing, and a new library takes a while to index on a CPU.

## Reaching the data

Cubeship has no console into an app. Anything that needs a shell is done over
SSH on the machine the app runs on, with `docker exec`. For example, to reset
the admin's password with Immich's own command:

```bash
docker exec -it $(docker ps -qf name=cubeship-immich-production-immich) \
  immich-admin reset-admin-password
```

or to open the database, which is a container of its own:

```bash
docker exec -it cubeship-db-immich-db psql -U cubeship immich
```

The internal names follow the project, environment and app names you install
with; they are on each app's page in the dashboard.

## The volumes

Each of the two apps runs as one copy on the machine its volume is on, and a
deploy stops it for a few seconds. While the server is stopped, the web app and
the phone apps cannot reach Immich.

**Uploads are the whole library.** The `/data` volume holds every original
photo and video, plus thumbnails and transcoded videos, which add 10–20% on
top. Size the server's disk for it.

Back up both the database and `/data`: the database holds the albums, people,
faces and which file is which asset, and restoring one without the other loses
that match. The database is backed up from its own page, with a schedule; a
volume backup stops the app for the whole copy, and
copies every file to an S3 store outside the instance — for a large library,
schedule it for when nobody is uploading, and expect it to take as long as
copying the library does. The `/cache` volume holds only models, which machine
learning downloads again.

Immich also dumps its own database into `/data/backups` every night at 2:00
and keeps the last 14 (Administration → Settings → Backup), so a backup of
`/data` carries a recent copy of the database with it.

## Resources

The server and machine learning are each limited to 2 GiB of memory and 2 CPUs.
The database has no ceiling until you set one on its page. Immich
asks for at least 6 GB of memory and 2 cores for the whole stack, and
recommends 8 GB and 4 cores: a VPS with 8 GB is the smallest to install this
on. Machine learning unloads a model after five minutes unused, and raising
its `limits` is what helps a large first import.

## Updating

Change both `tag`s in `template.yaml` to the new release. Immich migrates its
database when the server starts, and there is no going back: back up the
database and `/data` first, and read the
[release notes](https://github.com/immich-app/immich/releases).

A database's extensions are chosen when it is created, so a release of this
template that needed a different one would not change a database that already
exists. Cubeship says so in the update preview, and an extension can be
installed from the database's own page.
