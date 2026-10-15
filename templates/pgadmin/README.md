# pgAdmin on Cubeship

[pgAdmin](https://www.pgadmin.org) is the web-based administration tool for
PostgreSQL: browse schemas, run queries, and manage roles and backups from the
browser.

This template installs it on a Cubeship instance, with a volume for everything
pgAdmin keeps.

## What it creates

- **pgadmin** — pgAdmin, from `dpage/pgadmin4:9.17`, answering on the domain
  you choose, with a volume at `/var/lib/pgadmin`: accounts, saved servers,
  preferences, sessions and the files you upload.

It needs Cubeship 0.7.0 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where pgAdmin answers | A domain you control, pointed at your instance. |
| The email you sign in with | A real address. pgAdmin refuses special-use domains like `example.com` or `.local`. |
| The password you sign in with | Nothing — the instance generates it and shows it once. **Keep a copy.** |

## After installing

1. Open the domain and sign in with the email and the generated password.
2. Change the password under *your account → Change Password*.
3. *Register → Server* for each database. A database on the same instance
   answers at its internal host, `cubeship-db-<name>`, on port `5432`; its
   credentials are on its page in the dashboard.

The email and password only create the first account. Changing the variables
afterwards changes nothing.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Back the volume up from the app's settings: pgAdmin's
saved server passwords are in it.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Raise `limits` in
`template.yaml` if you need more.
