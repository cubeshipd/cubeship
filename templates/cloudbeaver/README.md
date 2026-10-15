# CloudBeaver on Cubeship

[CloudBeaver](https://dbeaver.com/cloudbeaver/) is DBeaver's web database
client: browse schemas, edit data and run SQL against PostgreSQL, MySQL,
MariaDB, MongoDB and dozens more, from the browser.

This template installs CloudBeaver Community on a Cubeship instance, already
configured, with a volume for everything it keeps.

## What it creates

- **cloudbeaver** — CloudBeaver, from `dbeaver/cloudbeaver:26.2.0`, answering
  on the domain you choose, with a volume at `/opt/cloudbeaver/workspace`: the
  server's configuration, its users, the connections you save with their
  passwords, and your scripts.

It needs Cubeship 0.7.0 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where CloudBeaver answers | A domain you control, pointed at your instance. |
| The administrator you sign in as | A lowercase name. Not `admin` or `user`: those are CloudBeaver's team names, and the account would not be created. |
| The administrator's password | Nothing — the instance generates it and shows it once. **Keep a copy.** |

## First start

CloudBeaver normally opens a setup wizard to whoever reaches it first, and
whoever finishes it becomes the administrator. This template skips it: the
server name, administrator name and password are set as variables, so the
server configures itself on its first start and creates that account before
it answers anyone. Anonymous access is off, so nothing is reachable without
signing in.

The variables only apply to that first start. Changing them afterwards
changes nothing.

## After installing

1. Open the domain and sign in with the administrator name and the generated
   password.
2. Change the password under *your account → Change password*.
3. *New connection* for each database. A database on the same instance
   answers at its internal host, `cubeship-db-<name>`, on the engine's port
   (`5432` for PostgreSQL, `3306` for MySQL and MariaDB, `27017` for MongoDB);
   its credentials are on its page in the dashboard.

## If you lose the administrator password

There is no console into an app on Cubeship. SSH into the machine the app runs
on and use `docker exec` on its container (`docker ps` lists it):

1. Open `/opt/cloudbeaver/workspace/.data/.cloudbeaver.runtime.conf` and
   delete the `"serverName"` line, keeping the JSON valid.
2. Redeploy the app. It starts in setup mode, and the wizard sets a new
   administrator password; connections and users are kept.

Until you finish the wizard, whoever opens the domain first can finish it
instead — do it straight after the redeploy.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. CloudBeaver Community keeps its own data in an embedded
H2 database inside the volume; an external database for it is a feature of
the paid editions. Back the volume up from the app's settings: the saved
connection passwords are in it.

## Resources

The app is limited to 1 CPU and 2 GiB of memory, and the JVM may use 75% of
it. Raise `limits` in `template.yaml` if you need more.
