# Managed databases

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

`internal/datastore` runs Postgres, MySQL, MariaDB, Redis and MongoDB
for the apps on this instance. Each is one entry in `specs` and nothing
else — adding the last two touched that map, and the service not at all.

**A database belongs to the instance, not to a project.** That is the
design decision, and everything follows from it.

It was inside an environment first, so that an app inherited its
connection string through the layering it was already in. That is true
and it was not enough: on one VPS the common shape is a single Postgres
serving several small apps, and those apps are routinely in different
projects. Owned by `web/production`, a database could not be reached
from `blog/production` at all — not because anything prevented it, but
because the model had decided in advance that it was the wrong thing to
want.

So ownership moved to the attachment, and 00019 is the migration that
moved it. A datastore exists on its own; `datastore_attachments` is the
whole of what connects it to anything, and it may cross projects and
environments freely.

**What is given up is that an environment no longer separates data by
itself.** `pg-production` and `pg-staging` are two datastores, told
apart by their names, and attaching the wrong one to the wrong app is
now possible where it used to be unrepresentable. That is a real cost,
paid for a database that can be shared — which is the reason to run one
on a box this size.

**It is a module of its own, not part of `app`.** A database has no
image to push, no source to build, no domain, no zero-downtime swap and
no deployments table. It is provisioned once and then runs.

**The name is the whole of the address.** Unique across the instance,
because it *is* the container: `cubeship-db-<slug>`, which is the host
every attached app resolves on the shared network. An app's container is
`cubeship-<project>-<env>-<name>-<nanos>`, so the two namespaces cannot
collide and a database called `api` may sit beside an app called `api`.
`engines` is the one refused name (`reservedSlugs`) — the API lists what
it can run at `/datastores/engines`, and Go's mux prefers the literal.

**The data is a host bind mount**, under `<data dir>/datastores/<id>`,
keyed by id rather than by name. Same rule as every other container
Cubeship runs: anything in a container's writable layer is destroyed the
next time its configuration changes, and changing the published port is
exactly such a change.

### How an app reaches one

By being **attached** to it. An attachment gives the app `DATABASE_URL`
and its parts, from its next deploy onwards — a container keeps the
environment it was created with, the same rule that makes adding a
domain take effect on redeploy.

The app is named by its **full reference**, because a datastore is not
inside an environment: `api` alone identifies nothing here, and two apps
called `api` in two projects may both be attached to one database.

**Two attachments collide when they name the same variables, and the
prefix is only half of that name.** The other half is the engine's
stem — `DATABASE` for the three that hold tables, `REDIS` and `MONGO`
for the two that do not (`Engine.VarStem`). A second Postgres on one app
therefore takes a `prefix` like `ANALYTICS_`, since two would be one
variable with two values; a Redis beside that Postgres takes nothing,
because `REDIS_URL` and `DATABASE_URL` cannot overwrite each other.

That is why the stem is **stored on the attachment row** and the unique
index is `(app_id, prefix, stem)`. A unique index cannot reach into
another table for the engine, and storing it is safe because an engine
is fixed for the life of a datastore. It was `(app_id, prefix)` first,
and attaching a cache to an app that already had a database was refused
for a conflict that does not exist. `PrefixTakenError` names the
variables rather than the prefix, because an app may hold several
databases and be colliding with exactly one of them.

The seam is `app.DatastoreVars`, declared in `app` and satisfied by
`datastore` — the dependency runs `app ← datastore`, and this is the one
thing that has to travel back. It is read fresh at every deploy rather
than stored on the app, so an attachment made after the last deploy is
picked up.

**Nothing below a datastore knows it exists.** Deleting a project takes
its apps and no database; the attachments to those apps go with them,
through the foreign key. A database outliving the apps that used it is
the point — it is the instance's, and deleting an app is not a decision
about anybody's data.

### What differs between engines

Four things, and each is a field on `spec` rather than a branch
somewhere:

- **How the password is delivered.** Every SQL engine takes it from the
  environment; Redis's official image has no variable for it, so it goes
  on the command line (`--requirepass`). `cmd` exists for that one case
  and `env` is nil there.
- **Whether the login is yours to choose.** Redis has one, called
  `default`, and the password belongs to it. Naming another is
  *refused*, not overwritten — silently ignoring what somebody typed is
  how a credential comes out different from what they thought they
  asked for. MySQL and MariaDB refuse `root` for the mirror reason: it
  already exists and this would not be its password.
- **Whether a named database means anything.** Redis's numbered
  databases are not the same idea and are not something to provision,
  so its variables carry no `_NAME`.
- **What the variables are called.** `stem` is `DATABASE` for the
  relational ones, `REDIS` and `MONGO` for the others — so an app with
  a Postgres *and* a Redis attached gets both, unprefixed, without
  colliding.

**None of them may move its data below the mount point.** Each of these
images chowns its data directory — and only its data directory — to the
unprivileged user it drops to. Point that at a subdirectory and the
mount above it keeps the mode the daemon created it with, `0700 root`,
which the engine's own user cannot traverse: the container comes up as
root, chowns something it can reach, drops privileges, and then cannot
open the directory it just prepared.

Postgres is where this bit. Its image documents pointing `PGDATA` at a
subdirectory when the data lives on a bind mount, and following that
advice broke every Postgres this module provisioned — `Permission
denied` on a restart loop, from a container that had been root a moment
earlier. `TestNoEngineMovesItsDataBelowTheMount` is what keeps the
advice from being taken again: it does not forbid the variable, it
requires the mount point as its value.

**And the value is said rather than left to the image.** Not setting
`PGDATA` was the rule for as long as the image's own default *was* the
mount, and 18 ended that: its default moved to
`/var/lib/postgresql/<major>/docker`, with the volume declared one level
above at `/var/lib/postgresql`. Left alone, a Postgres 18 datastore
comes up, works, and keeps its data in an anonymous volume nothing on
this instance names — so it is in no backup of the data directory and it
is orphaned the next time the container is replaced, which is what
publishing a port does. `postgresDataPath` is both the bind mount and
`PGDATA`, one fact rather than two, and `TestPostgresSaysWhereItsDataGoes`
is there because adding a tag to the list is exactly the edit that would
otherwise lose somebody's database in silence.

Two smaller ones worth knowing: Redis is started with `--appendonly
yes`, because otherwise it snapshots on its own schedule and a restart
loses the last few minutes — free for a cache, and somebody's queue
otherwise. And Mongo's connection string carries `authSource=admin`,
because its root user lives in the `admin` database whatever database
the connection names; without it every connection fails on credentials
that are perfectly correct.

### Turning one off

`POST /datastores/{name}/stop` stops the container and leaves it, and
its data, where they are. **Stopped rather than removed, so the log
survives** — what somebody wants immediately after turning a database
off is usually the reason they turned it off.

The status becomes `stopped`, not `down`, and the reconciler leaves it
alone: Docker's restart policy is `unless-stopped`, so it is still off
on purpose after a reboot, and rewriting that to `down` would turn a
decision into what looks like a fault on every daemon start.

`start` provisions again rather than starting the container that is
already there — one path instead of two, the data being a bind mount a
recreate does not touch. It is also how a datastore whose provisioning
failed is retried.

`has_container` on the response is what says whether there is a log to
read or anything to stop. The status cannot answer it: a datastore whose
provisioning failed may have neither.

### Fixed after creation, and why each one is

- **The name.** It is the container's, which every attached app resolves.
- **The engine and the version.** A data directory written by one major
  version is not readable by another. A datastore that "changed version"
  would be a container that will not start, with the only copy of the
  data inside the directory it will not read. Running a new version
  means a second datastore and the engine's own migration tools.
- **The password.** It is used once, when the engine initializes itself,
  and nothing reads the column afterwards. Changing it would change
  every connection string Cubeship hands out while the database went on
  accepting only the old one.

The description and its **limits** are what is left, which is why
`PATCH` takes two fields. With no project above it to say where a
database belongs, this is the only place a description can go; the
ceiling is here because a database is the container on a box this size
most worth capping — an app that leaks is one app, and a Postgres that
takes every page of memory takes the daemon and the proxy with it, and
unlike an app it is not restarted by a deploy somebody was about to do
anyway. See "What a container may take".

### Credentials

Stored as given, like an external registry's login and for the same
reason: a hash cannot connect to anything. Never in a listing —
`GET /datastores/{name}/credentials` is its own request and an admin's.
Generated when a request carries no password, so a database with a weak
one is not something anybody gets by leaving a box empty; the dashboard
generates its own and shows it, because a field somebody has to fill in
is a field somebody fills in badly.

`Datastore.URI` builds the connection string through `net/url`, so a
chosen password containing `@` or `/` is escaped rather than producing a
URL that parses as a different host.

**The MCP tools stop short of two things**, and the line is the one
`internal/extregistry` draws by having no tools at all: no tool reads or
sets a password, and no tool exposes a datastore. An agent can provision
a database, attach an app and never hold the credential, because the app
receives it through its environment.

### Exposing one

Off by default. `POST /datastores/{name}/expose` publishes it on a host
port — from `PortRangeStart`-`PortRangeEnd` unless one is named — for a
migration run from a laptop, psql, a BI tool.

**Not through Traefik.** Traefik routes HTTP by host name; a database
speaks its own protocol on its own port, and a TCP router matching
`HostSNI(*)` can only have one backend per entrypoint, so two exposed
databases of one engine could not share one. The container publishes the
port itself.

That means there is **no TLS**, and the endpoint says so. What makes an
exposed database safe is the password and a firewall rule, and the second
is the operator's. Publishing replaces the container to pick the port up
— published ports are fixed at create time — and the data survives
because it is a bind mount.

Ports 15000-15999 for the automatic range: the daemon's own Postgres
already publishes 5432 on loopback, so the obvious number is the one
number that cannot work.

### In the dashboard

`/databases` is its own section in the sidebar, beside Projects rather
than under Platform: a database belongs to the instance, but it is a
thing you deploy against, not a thing the instance is wired to, and it
is opened as often as an app is.

The list is a **table**, like the registries and the DNS accounts: what
someone comes here to do is scan a column — which engine, is it up, what
is using it — and cards make you read each one whole to find the line
you were after.

**Every page for one running thing is tabs, and the first tab is
always the same two questions**: is it working, and how do I reach it.
An app opens on Overview, Environment, Logs; a database on Overview,
Apps, Logs; an object store on Overview, Buckets, Apps, Logs. Monitoring
comes first because it is the question you have before you know you have
one, and how to connect is beside it because it is short, answered once,
and the reason somebody opened the page. A **linked** store's Overview
is the connection alone: there is no container here to chart.

Everything else is a tab, and the test is length or depth: an app's
environment is fifty rows and its log five thousand lines; a database's
attached apps and a store's buckets are tables with their own dialogs
and their own confirmations. Stacked as sections they push the two
questions above off the screen, and they do it exactly when the thing is
busy enough to be worth looking at.

A database's page was sections for a while, on the grounds that all
three were short. Two of them are not, and a page that opened on a
scroll past a log to reach a chart is what that argument came to.

A tab that could never have content is **not offered**, and one whose
content does not exist *yet* is offered and disabled with the reason on
hover — the difference between a linked store, whose log is on somebody
else's machine, and a managed one that has not come up. A disabled
control that explains nothing is one somebody clicks twice.

The connection details are **fields with a copy button** rather than a
table of values. A connection string is long, and a field bounds it and
keeps it on one line where a wrapped paragraph gives you three and a
chance to miss one.

`CopyField` is `disabled` and `user-select: none`. It was `readOnly`
first, focusing and selecting itself on a click — so clicking its
*label* lit the focus ring and highlighted the value, which reads as an
edit about to happen on something that cannot be edited. The button is
how a value leaves the field; nothing has to take focus for that, and a
selection nobody asked for is only ever half a connection string. The
dimming a disabled control normally gets is undone for these in
`globals.css`, where every other field rule lives: dimming says
"unavailable for now", and this is not unavailable, it is final.

Settings stays on its own page like every other resource, because the
actions that cannot be undone belong at the bottom of a page you went to
on purpose. It holds two things and no more — publishing on a host port,
and deleting. There was a "general" section above them showing the name,
the engine and the login; everything in it was already on the database's
own page one click away, and a settings screen that mostly restates
another screen teaches people that settings screens are where facts
live.

### What is not here

**Backups are their own module now** — see "Backups" below. What is not
here is backing up anything that is *not* a datastore, Cubeship's own
Postgres included.

**Rotating a password**, for the reason above: it would take an
`ALTER USER` inside the database, not a column write.
