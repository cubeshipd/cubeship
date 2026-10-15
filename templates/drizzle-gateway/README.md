# Drizzle Gateway on Cubeship

[Drizzle Gateway](https://gateway.drizzle.team) is Drizzle Studio, self-hosted:
a database browser for PostgreSQL, MySQL, MariaDB, SQLite and more, where you
browse and edit rows, run SQL, and manage schemas, roles and policies from the
browser. It is free, made by the Drizzle team, and not open source.

This template installs it on a Cubeship instance, behind a master password,
with a volume for everything it keeps.

## What it creates

- **drizzle-gateway** — Drizzle Gateway, from
  `ghcr.io/drizzle-team/gateway:1.6.2`, answering on the domain you choose,
  with a volume at `/app`: the saved connections, their credentials, the
  passcode for other users and the key that signs sessions.

It needs Cubeship 0.7.0 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Drizzle Gateway answers | A domain you control, pointed at your instance. |
| The master password you sign in with | Nothing — the instance generates it and shows it once. **Keep a copy.** |

## After installing

1. Open the domain and sign in with the master password.
2. Add a connection for each database. A database on the same instance
   answers at its internal host, `cubeship-db-<name>`, on its engine's port
   (`5432` for PostgreSQL, `3306` for MySQL and MariaDB); its credentials are
   on its page in the dashboard. The gateway reaches only what its container
   can: databases on the instance, and anything reachable over the internet.
3. To let others in without the master password, set a passcode in the
   gateway's settings. A passcode opens the databases but cannot change the
   passcode or read the app's variables.

The master password is the `MASTERPASS` variable. To change it, edit the
variable and redeploy; whoever signed in with the old one is signed out.
Remove the variable and the gateway asks for no password at all — anyone who
finds the domain gets every saved database.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Back the volume up from the app's settings: the
password of every saved connection is in it.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Large result sets and
exports are held in memory; raise `limits` in `template.yaml` if you need more.
