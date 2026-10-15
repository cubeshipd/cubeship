# Metabase on Cubeship

[Metabase](https://www.metabase.com) is an open-source business intelligence
tool: connect your databases, ask questions without writing SQL, and share
the answers as charts and dashboards.

This template installs the open-source edition on a Cubeship instance with a
managed Postgres for its own application data.

## What it creates

- **metabase** — the Metabase server, from `metabase/metabase:v0.63.17.2`,
  answering on the domain you choose.
- **metabase-db** — a managed Postgres 18 database, attached to the app. It
  holds Metabase's users, questions, dashboards and saved connections — never
  the embedded H2 file, which is not for production.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Metabase answers | A domain you control, pointed at your instance. |
| The key Metabase encrypts saved database credentials with | Nothing — the instance generates it and shows it once. **Keep a copy, and never change it**: under a different key, the credentials of every database you connected cannot be read. |

## After installing

1. **Open the domain straight away.** The first visit is the setup wizard,
   and whoever reaches it first creates the admin account.
2. The first start migrates the database and takes a minute or two; the domain
   answers once it is done.
3. Connect your data under *Admin → Databases → Add database*. A database on
   the same instance answers at its internal host, `cubeship-db-<name>`, on its
   engine's default port — `5432` for Postgres; its credentials are on its page
   in the dashboard.

`MB_SITE_URL` assumes the instance serves HTTPS. On an instance with TLS off,
change it to start with `http://`.

Anonymous usage tracking is off (`MB_ANON_TRACKING_ENABLED=false`).

## What is not kept

The app has no disk of its own. Everything Metabase keeps is in Postgres, but
a third-party driver copied into `/plugins` is gone on the next deploy. The
drivers Metabase ships with need nothing.

## Resources

The app is limited to 1 CPU and 2 GiB of memory, and the JVM sizes its heap
from that limit. If Metabase logs `OutOfMemoryError: Java heap space`, raise
`limits` in `template.yaml`, or set `JAVA_OPTS` to `-Xmx1500m` or similar,
below the limit.
