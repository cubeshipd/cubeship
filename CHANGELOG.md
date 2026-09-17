# Changelog

Every release of Cubeship, newest first.

<!-- Generated from internal/release/notes by tools/changelog. Edit a note
     there and run `make changelog`; editing this file is editing the
     copy rather than the thing. -->

## 0.10.0 — 2026-09-17

A shell inside any app's container, and a root shell on any machine in the instance — from the dashboard or the CLI, with no SSH port open.

### Shells

**A terminal inside an app's container.** An app's new **Shell** tab, or
`cubeship app shell <app>`, opens `bash` (or `sh`) in its running
container, as its own user and with its environment — `docker exec -it`
without signing in to the machine. On an app spread over several
machines, pick which copy; `--server` does the same from the CLI, which
exits with the shell's own status.

**A root shell on a machine.** **Servers → Open a root shell**, or
`cubeship server shell <name>`, is the same as SSH as root on that box: its
files, its services, its Docker. The dashboard asks for your password again
before it opens.

- **Workers too, with no port opened.** A worker is told to open the shell
  on its next poll and connects the session back to the control plane, the
  way it already reaches it for everything else.
- **Who can.** A root shell is an admin's alone. A shell in an app is its
  own permission — admins have it; a role grants it with the new **Shell**
  switch on apps, which needs manage. **No existing role gets it on
  upgrade**, including Deploy: a shell reads every secret the container
  holds.
- **In the audit log.** Every session is recorded when it opens and when it
  closes, with how it ended. What is typed into it is not.
- **How one ends.** Closing the tab or the terminal ends the session and
  hangs up what was running in it. Thirty minutes with no key pressed and
  nothing printed closes it too.
- An image with no shell at all — distroless, or built from scratch — is
  refused with that reason.

### Upgrade notes

Update normally from the dashboard or CLI. No database migrations. A shell
on a worker needs that worker on 0.10.0; one still on an older version is
refused with that reason until it updates. Updating the instance closes
any shell that is open at the time.

## 0.9.3 — 2026-09-16

An agent can now manage the names an app answers at, including the port each one reaches — the last piece of an app's configuration that needed a person and a mouse.

### Added

**Domains over MCP.** Four tools, so an agent can finish setting an app up
without handing it back: `list_app_domains`, `add_app_domain`,
`set_app_domain_port` and `remove_app_domain`.

- **The port is the one that matters.** A name pointing at a port nothing
  listens on is a 502 from a container that is perfectly healthy — the
  usual cause being an image that moved where it listens. Changing it kept
  the name and its certificate; there was no tool for it, so the fix was a
  click somebody had to make.
- **A domain is named by its host**, not by the id the HTTP routes take. An
  agent has the name already, and asking it to list the domains to turn
  that name into a number is a step that can only go wrong.
- Adding, changing and removing require the admin role. Listing needs only
  the ability to see the app, and an account that cannot change a domain is
  not shown the tools that do.
- Every change is in the audit log, by host.

Each of them takes effect on the app's next deploy, which is the rule every
routing change already followed.

### Upgrade notes

Update normally from the dashboard or CLI. No database migrations. Your
agent picks the new tools up when it reconnects.

## 0.9.2 — 2026-09-16

Redeploying an app you push images to keeps the version it is running, instead of asking the registry for a "latest" nobody pushed.

### Fixed

**Deploy now means "this app again".** An app that follows this instance's
registry is deployed by `docker push`, under whatever tag you pushed — a
commit SHA, usually. Pressing **Deploy** without naming a tag asked for
`latest` instead, which nothing had ever pushed, so changing an environment
variable and redeploying failed with *not found*. It now redeploys the tag
the app is already running.

- Nothing else moves. An app pinned to a tag still deploys that tag, a
  source that builds still uses its stored ref, and an app that has never
  deployed still falls back to `latest`.
- The tag it keeps is the one from the newest deploy that worked — the same
  version a machine falls back to on its own, so a redeploy and a rollback
  cannot disagree.
- `POST /apps/{...}/deployments` with no body and the `deploy_app` tool
  changed with it. `cubeship app deploy --tag` no longer defaults to
  `latest`, so leaving it out now means the same thing there.

### Upgrade notes

Update normally from the dashboard or CLI. No database migrations. An app
left on a failed `latest` deploy recovers on its next one — it reads past
the failures to the version that worked.

## 0.9.1 — 2026-09-16

Agents that validate an MCP server strictly can see this instance's tools again — twelve of them made the whole list unreadable.

### Fixed

**The MCP tool list is valid again.** Twelve tools that return a list —
`list_apps`, `list_projects`, `list_datastores`, `list_servers`,
`get_app_deployments` and the rest — described their result in a shape the
MCP specification does not allow. A client that checks the list against the
specification, which most Python-based agents do, rejected **every** tool at
once over it, with an error naming only a number. Anything more forgiving,
Claude Code included, never noticed.

- **Those tools now answer with an `items` field** holding what they used to
  return directly. A client that reads the results itself, rather than
  through an agent, reads `items`.
- An empty result is an empty list, where some of them used to send nothing
  at all.

### Upgrade notes

Update normally from the dashboard or CLI. No database migrations. Point
your agent at `https://<your-domain>/mcp` again after the daemon restarts.

## 0.9.0 — 2026-09-15

Postgres extensions on managed databases — pgvector, VectorChord and PostgreSQL's contrib modules — installable at creation or afterwards, and declarable by a template.

### Extensions on a managed Postgres

- **Twenty-six extensions, from a reviewed list.** `pgvector` and
  `vectorchord` for embeddings and vector search, plus PostgreSQL's own contrib
  modules: `pg_trgm`, `pgcrypto`, `hstore`, `citext`, `ltree`, `unaccent`,
  `uuid-ossp`, `btree_gin`, `btree_gist`, `bloom`, `cube`, `earthdistance`,
  `fuzzystrmatch`, `intarray`, `isn`, `seg`, `tablefunc`, `tsm_system_rows`,
  `dblink`, `postgres_fdw`, `pg_prewarm`, `pgstattuple`, `pg_buffercache` and
  `pg_stat_statements`.
- **Install them whenever you need them.** A database's **Extensions** tab
  lists what it has and what it can have, with a filter. Most of them are
  already inside the image, so installing one is a single statement and the
  database keeps answering; `pgvector`, `vectorchord` and `pg_stat_statements`
  need a different image or a preloaded library, so those replace the container
  for a few seconds. Your data is a directory on the host and is not touched.
- **An extension cannot be removed.** A column's type, an index or a default
  may already depend on it, so Cubeship adds and never takes away.
- **Only names Cubeship knows.** A name picks a fixed `CREATE EXTENSION` and,
  where one is needed, an image pinned by digest and built for both amd64 and
  arm64. No image, package or SQL from a request or a template ever reaches the
  database.
- **Everywhere you already work.** `POST /datastores` takes `extensions`,
  `POST /datastores/{name}/extensions` installs more,
  `GET /datastores/engines` lists what each version offers, `cubeship db create
  --extension` and `cubeship db extension add` do the same from a terminal, and
  an agent has `add_datastore_extensions` over MCP.

### Templates can ask for them

- **`databases[].extensions`.** A template declares what its database needs and
  the install creates it that way — `extensions: [pgvector, vectorchord]` for an
  Immich, `[pgvector]` for a Honcho. The list is normalized: sorted,
  deduplicated, and carrying what an extension requires, so `[vectorchord]` and
  `[pgvector, vectorchord]` are the same template.
- **A template with extensions needs `minCubeship: "0.9.0"`.** An older
  instance would install it and create the database without them, which is an
  app that comes up and fails on its first query.
- **A template update never changes an existing database.** The preview says
  what the new release asks for, the same way it does for an engine or a
  version, and you install them on the database yourself.

### Upgrade notes

Databases created before this release have no extensions and keep running the
image they came up on. Nothing about them changes.

The extensions are Postgres's. The other engines Cubeship runs are unaffected,
and so is the PostgreSQL that Cubeship keeps its own state in.

## 0.8.2 — 2026-09-15

Apps on the instance can reach it by its own domain, so an agent running as an app can use its MCP.

### Reach the instance from its own apps

- **The instance's domain works from inside.** An app calling
  `https://<your-domain>` — the API, `/mcp` or the registry — now stays on
  the machine instead of leaving for its public address, which most hosts
  never route back in. An agent running as an app, such as Hermes Agent,
  connects to `https://<your-domain>/mcp` exactly as it would from a laptop,
  with the real certificate.
- Another app's domain still leaves the machine. Use its internal address
  for that.

### Upgrade notes

Update normally from the dashboard or CLI. The proxy is recreated once when
the new version starts, so apps are unreachable for a few seconds. Changing
the instance's domain later recreates it the same way. No database
migrations and no redeploy needed.

## 0.8.1 — 2026-09-14

Handle sign-out failures gracefully and restore your theme when saving fails.

### Dashboard fixes

- **Sign out without a broken screen.** If the sign-out request fails, the
  dashboard shows the error and lets you try again instead of raising an
  unhandled runtime error. Repeated clicks are disabled while signing out.
- **Theme changes recover cleanly.** If saving a palette fails, the previous
  theme is restored and the dashboard explains the failure. Palette changes
  are held while a save is in progress to prevent overlapping requests.

### Upgrade notes

Update normally from the dashboard or CLI. This patch adds no database
migrations or configuration changes.

## 0.8.0 — 2026-09-14

A redesigned dashboard, smoother navigation, personal avatars, per-machine monitoring and platform component logs.

### A new Cubeship experience

- **A redesigned dashboard.** A consistent cyberpunk interface brings clearer
  navigation, refined tables and cards, connected tabs and a responsive sidebar.
- **Smoother navigation.** Page transitions provide immediate loading feedback,
  reuse cached data and keep the application shell in place. Saving account
  settings no longer reloads the whole interface.
- **Your own profile image.** Upload, crop, replace or remove your avatar from
  Account. Initials replace the old character presets when no image is set.
- **The complete template catalog.** Browse all matching templates without
  infinite scrolling, with search, sorting and filters across the full result.

### Know what every machine is doing

- **Choose a machine in Overview.** CPU, memory, disk and network charts show
  the selected control plane or worker. Project and workload totals remain
  instance-wide, and stale measurements are identified explicitly.
- **Platform → Components.** Administrators can inspect Cubeship's own
  containers on each machine, including their state, image, health and restarts.
  View and follow logs for the daemon, dashboard, Traefik, database, registry
  and builder where those components run.
- **Worker telemetry travels through the existing agent connection.** No
  additional public ports or external monitoring service are required.

### Website and documentation

- A new product landing page, interactive infrastructure scenes and refreshed
  documentation and comparison pages bring the website into the same identity.
- Template detail pages keep long README content, tables and source code within
  their columns. The README banner and social previews use the new visual style.

### Upgrade notes

Upgrade the control plane and workers to 0.8.0 to use worker monitoring and
component logs. Workers on older versions cannot provide the new reports.
Database migrations run automatically at startup. Worker chart history begins
when the upgraded worker sends its first measurements.

## 0.7.3 — 2026-09-14

Security fixes isolate private infrastructure, restrict inherited environment edits and worker image access, and bound password login work.

### Security

- **Private infrastructure has its own network.** The daemon, its database,
  builder, registry and dashboard move to a management bridge during startup.
  Traefik and managed object stores retain the connections their clients need.
- **Inherited environment edits require an administrator.** Project and
  environment variables can influence source builds, so member accounts cannot
  change them even with a key granting project management.
- **Password login limits resource use.** Request sizes, password lengths and
  concurrent login attempts are bounded; excessive attempts receive HTTP 429.
- **Workers can pull only assigned repositories** from the instance registry.
- **Recovery HTTP binds to localhost** on installation and built-in updates.
  Use HTTPS normally, or an SSH tunnel for recovery.
- **New app containers drop raw socket capability and prevent privilege
  escalation.** Redeploy existing apps to apply these runtime restrictions.

### Upgrade notes

Network migration happens at daemon startup; a failed migration stops startup
rather than leaving private services exposed. Linked S3 endpoints using an
application-only Docker name need an endpoint reachable from the management
network. Passwords exceeding 1024 bytes require an administrator reset.
Custom daemon installations using host networking require reinstallation on
the management bridge. See the installation documentation for SSH recovery.

## 0.7.2 — 2026-09-14

Apps can publish TCP ports for protocols that are not HTTP, such as SSH into a Git server.

### Added

**TCP ports for apps.** A port of an app's container can be published on a
port of the instance's own address — SSH into GitLab or Gitea, a game
server, a broker. Add one under the app's **Settings → Network**, or with
`cubeship app tcp add <app> <port>`; a template declares them with
`apps[].tcp`.

- **Nothing sits in front of it.** No TLS and no proxy: the app's own
  authentication protects what it serves there. On a host whose Docker
  ports are under ufw, Cubeship admits the port.
- **An app with a TCP port runs as one copy on the control plane**, and each
  deploy stops the old container before starting the new one, so the app is
  briefly unavailable.
- **Host ports are 1024 and up**, so 22 stays the server's own SSH. Left
  empty, one is picked from 17000–17999.
- The port opens on the app's next deploy.

## 0.7.1 — 2026-09-14

The template catalog loads more templates as you scroll, instead of stopping at the first 24.

### Fixed

**The catalog stopped at 24 templates.** Templates showed the first page
of the catalog and nothing after it. The next page now loads as the grid
is scrolled to its end, and changing the search, tag or order starts again
from the top.

## 0.7.0 — 2026-09-13

Templates install whole stacks in one form, apps can keep data in volumes, access roles decide what every account and API key reaches, and an audit log records who changed what.

### Upgrading

**Every account gets an access role.** Admins get **Admin**; members get
**Deploy**, which is exactly what a member could do before. Nothing an
account or its keys could do changes. See Access roles below.

**A deploy stuck in `pending` from an earlier restart is closed** the first
time the instance starts on this release, and can then be deleted.

### Added

**Templates.** A template is a GitHub repository with a `template.yaml`
describing apps, the databases and object stores they need, and the
questions an installer answers. The catalog at
[cubeship.dev/templates](https://cubeship.dev/templates) indexes their
releases, and **Templates** in the dashboard reads it.

- **Installing is one form.** Pick a version, a project and environment —
  existing ones, or new — and answer the template's questions. A domain
  can be set through a connected DNS provider, which writes the record
  before the install starts. The instance creates every database, store
  and app, wires them together, deploys them, and waits for every build.
- **A failed step undoes the whole install.**
- **Installed says when a newer release exists.** Updating shows what will
  be created, changed and kept first, can move to any release — older ones
  included — and never deletes. A failed update puts the apps back.
- **Uninstalling keeps the data by default.**
- Only an admin, or a role that manages templates, installs. The same is
  in `cubeship template` and the MCP tools. Opening Templates is the only
  thing that reaches cubeship.dev, with nothing about your instance in it.

**Volumes for apps.** A path inside an app's container whose contents
survive deploys and restarts — what RabbitMQ, Elasticsearch and anything
that keeps state in files needs. Add one under the app's
**Settings → Volumes** or with `cubeship app volume add`.

- **An app with a volume runs as one copy on one server**, where its data
  is, and each deploy stops the old container before starting the new one.
- **A new volume is writable by the app**, taking the owner the image has
  at its path.
- **The data outlives the volume by default**; kept data is listed until
  an admin removes it.
- **Backed up and restored** now or on a schedule. On the control plane
  to its disk or S3; on any other server, the worker itself sends it to an
  S3 bucket outside the instance. A restore is unpacked beside the data
  first, so a failed one changes nothing.
- **A volume moves to another server by restoring its backup there.**
- **Templates can declare volumes**, with `minCubeship: "0.7.0"`.

**Access roles.** Every account holds a role: for each kind of resource —
projects, apps, domains, databases, object storage, servers, templates,
backups, registries, git and DNS providers, credentials, certificates, the
firewall, settings, the audit log — a level (view or manage), whether it
reads secrets, and which projects, databases or stores. **Admin**,
**Deploy** and **Read only** ship with the instance and cannot be changed;
others are made on **Users → Access roles**. Users and roles, and building
source, stay an admin's.

**Roles for API keys.** A key given a role reaches what the role grants and
its owner reaches — never more. Over MCP a key is not offered a tool its
role would refuse, so an agent cannot be talked into calling one.
`cubeship user api-key create --role`, `cubeship role list`, `list_roles`.

**The audit log.** Every change made through the dashboard, the API or MCP,
and every refused attempt, as a sentence: who, through which door, with
which key, and how it ended. Filter by person, channel, outcome and date
range on **Platform → Audit log**, or with `cubeship audit` and
`list_audit_events`. Request bodies are never kept; events are kept 90
days.

**Beta versions, if you ask for them.** **Settings → Updates → Receive beta
versions** makes the update button offer betas and release candidates.
Automatic updates stay on stable releases, and turning it off never goes
back a version.

**Cards show what everything is using.** Projects, apps, databases and
object stores carry CPU and memory bars.

**An app's addresses can be copied and opened from its page**, internal
address and public domains alike.

### Fixed

**An exposed database or object store could be unreachable, or open more
than it should.** Exposing one never told the firewall, and a hand-written
rule opened every database of that engine. Cubeship keeps its own block in
ufw's `after.rules` now, per published port — nothing to do on your side.

**A build no longer runs out of time on a small VPS.** A deploy that builds
gets its own 30 minutes.

**An app calling another by its public domain hung** when both were on the
same machine.

**A deploy interrupted by a restart stayed `pending` for ever.**

**Prereleases are ordered part by part**, so `rc.10` comes after `rc.2`.

**The release notes open at the newest release**, and a stable version
shows only stable notes.

**An update or uninstall started from an installation's page shows its
progress**, and a chosen theme stays chosen when Appearance is opened
again.

### Security

**An ECR region is checked where it becomes an address**, so one saved
before 0.6.0 cannot end the AWS hostname early.

**Secrets a template install generates are no longer written to the
browser's session storage.**

## 0.7.0-rc.11 — 2026-09-13

*Prerelease.*

Access roles decide what every account and API key reaches, and an audit log records who changed what.

### Added

**Access roles.** Every account holds a role: for each kind of resource —
projects, apps, domains, databases, object storage, servers, templates,
backups, registries, git and DNS providers, credentials, certificates, the
firewall, settings, the audit log — a level (view or manage), whether it
reads secrets, and which projects, databases or stores. **Admin**, **Deploy**
and **Read only** ship with the instance and cannot be changed; others are
made on **Users → Access roles**. Admins keep Admin, and members become
Deploy, which is exactly what a member could do.

**Roles for API keys.** A key can be given a role, and then reaches what the
role grants and its owner reaches — never more. Over MCP a key is not
offered a tool its role would refuse, so an agent cannot be talked into
calling one. `cubeship user api-key create --role`, `cubeship role list`,
`list_roles`.

**The audit log.** Every change made through the dashboard, the API or MCP,
and every refused attempt, in a sentence — who, through which door, with
which key, and how it ended. Filter by person, channel, outcome and a date
range on **Platform → Audit log**, or with `cubeship audit` and
`list_audit_events`. Request bodies are never kept; events are kept 90 days.

### Fixed

**A chosen theme stays chosen.** Opening Appearance again put the default
palette back.

## 0.7.0-rc.10 — 2026-09-13

*Prerelease.*

A template's domain can be set through a DNS provider, and its project picked from the ones there are.

### Added

**A template's domain through a DNS provider.** A domain question in the
install form offers what adding a domain to an app does: a connected DNS
provider, a zone and a subdomain — the `A` record pointing at this
instance is written before the install starts — a name under the
instance's own domain, or one typed by hand.

**Install into a project that exists.** The form lists the instance's
projects beside **New project**, and inside an existing one its
environments beside a new one, rather than asking for names to type.

### Fixed

**The release notes open at the newest release.** They opened scrolled to
the first link in an older release's notes.

**The install form has no gaps.** Fields are laid out so none sits alone
beside an empty space, and a domain question is spaced like the rest.

## 0.7.0-rc.9 — 2026-09-13

*Prerelease.*

A deploy that builds gets its own 30 minutes, and a template install waits for every app it builds.

### Fixed

**A build no longer runs out of time on a small VPS.** A deploy had ten
minutes for everything, and for an app built from a repository that
covered cloning, pulling the base image, building and loading the image —
which a cold build on a 1–2 GB base image does not finish. The build now
has 30 minutes of its own, and the pull, the swap and the health check
keep their ten minutes after it. A build that runs out says so in the
deployment's error rather than `context deadline exceeded`.

**A template install waits for every app it builds.** An install or update
had half an hour in total, so a template whose second built app was still
building was undone — its volumes included. It now gets half an hour plus
30 minutes for each app built from a repository. `cubeship app deploy` and
the other commands that wait on a deploy keep watching for as long as the
daemon lets it run.

## 0.7.0-rc.8 — 2026-09-13

*Prerelease.*

Install a template at any version, and move an installation to another one — older ones included.

### Added

**Choose the version a template is installed at.** A template's page
has a **Version** picker listing every release the catalog accepted, the
newest first. Choosing an older one shows what that version creates and
asks, read from its repository the way installing it reads it, and says
so when it needs a newer Cubeship than this instance runs.
`cubeship template releases <owner/repo>` and the MCP tool
`list_template_releases` list the same versions.

**Choose the version an installation updates to.** The update dialog has
the same picker, marking the version installed and the newest. An older
one moves the installation back: the apps get that release's images and
settings, nothing is deleted, and what newer releases created stays.

### Changed

**A stable version shows only stable release notes.** Updating from
0.6.0 to 0.7.0 shows the notes of 0.7.0, which cover the whole release,
rather than one page for every release candidate on the way. An instance
running a beta or a release candidate still sees each one.

## 0.7.0-rc.7 — 2026-09-13

*Prerelease.*

Volumes on any server are backed up, only to S3 outside the instance, and a volume moves to another server by restoring its backup there.

### Changed

**A volume is backed up only to an S3 store linked from outside the
instance.** Not this machine's disk, and not a store this instance runs,
which is the same disk. A database is still dumped locally, to be loaded
back or looked at; a volume's copy is only worth taking where it survives
the machine. **Back up now** asks which store and bucket, the schedule
offers only those stores, and `cubeship app volume backup take` takes
`--store` and `--bucket`. Backups already taken to the disk still list
and restore.

### Added

**Volumes on other servers are backed up and restored.** The server the
volume is on makes the archive and sends it straight to the bucket, so
the data never passes through the control plane. The app is stopped for
the copy and started again afterwards, as on the control plane. A server
that has not called in for two minutes is refused, and one on an older
version says it does not know the command — update it first.

**A volume moves to another server by restoring its backup there.**
Choose the server when restoring (**Restore on**, or `cubeship app volume
backup restore <id> --server <name>`): the backup is restored on that
server, then the app is placed there and starts on it. Anything written
after the backup was taken is not moved, and the copy on the old server
is left where it is. An app with more than one volume cannot be moved
this way yet.

### Fixed

**A backup interrupted by a daemon restart no longer stays `taking` for
ever.** It is marked failed when the daemon starts, so it can be deleted.

## 0.7.0-rc.6 — 2026-09-13

*Prerelease.*

Apps can have volumes — directories that survive deploys — so a queue or a search index can run on Cubeship and ship as a template.

### Added

**Volumes for apps.** A volume is a path inside an app's container whose
contents survive deploys and restarts, which is what RabbitMQ,
Elasticsearch and anything else that keeps its state in files needs. Add
one under the app's **Settings → Volumes**, with `cubeship app volume
add`, or through the API; it is mounted from the app's next deploy.

**An app with a volume runs as one copy on one server**, where its data
is. Scale above one, spreading and autoscaling are refused while it has
one, and each deploy stops the old container before starting the new
one, so the app is unavailable for a few seconds per deploy. A new
container that will not start is removed and the old one started again.

**A new volume is writable by the app.** Before its first data, a
volume takes the owner and mode the image has at its path — or the user
the image runs as — so an image that does not run as root can write to
it from its first start.

**The data outlives the volume by default.** Removing a volume or
deleting its app keeps the data unless you ask for it to be deleted, and
kept data is listed until an admin removes it.

**Volumes are backed up and restored** from the same Volumes tab: now,
or every day on a schedule, to this machine's disk or an S3 bucket, with
the same retention as a database's backups. The app is stopped while
each copy is taken and started again afterwards. A restore replaces the
volume's contents and cannot be undone; it is unpacked beside the data
first, so one that fails leaves the data as it was. Volumes on a server
other than the control plane cannot be backed up yet.

Volume backups are also in `cubeship app volume backup list|take|restore`,
in the MCP tools `list_volume_backups` and `back_up_volume`, and as rows
of the coverage report under **Backups**, beside the databases.

**Templates can declare volumes**: `apps[].volumes: [{ path }]`. A
template with one needs `minCubeship: "0.7.0"`, so an older instance
refuses it rather than installing it without its data surviving.
Uninstalling keeps the data unless you say otherwise.

## 0.7.0-rc.5 — 2026-09-13

*Prerelease.*

A deploy a daemon restart interrupted no longer stays pending for ever, and two security fixes found by code scanning.

### Fixed

**A deploy interrupted by a restart stayed `pending` for ever.** A
deploy's build and the swap of its container run inside the daemon that
started it, so updating the instance while a site was building left the
deploy saying `pending` with nothing working on it — and a deploy that
has not finished cannot be deleted.

The daemon now looks at every pending deploy when it starts. One whose
image was never built, or whose container on this machine was never
swapped, is marked failed with a line saying to deploy again; one every
copy is already running is marked succeeded; one waiting on another
machine of a cluster is left for that machine, as before. **A deploy
stuck from before this release is closed the first time the instance
starts on it**, and can then be deleted.

### Security

**An ECR region is checked where it becomes an address.** Regions have
been checked where they are typed since 0.6.0; a registry saved before
that could still carry one that ended the AWS hostname early. The daemon
now refuses such a region before it signs a request with it.

**Secrets a template install generates are no longer written to the
browser's session storage.** They are handed to the page that shows them
in memory, so nothing another script on the page could read is left
behind. Reloading that page loses them, which closing it already did.

## 0.7.0-rc.4 — 2026-09-13

*Prerelease.*

An app can reach another on the same machine by its public domain, and a project a template creates wears the template's icon.

### Fixed

**An app calling another by its public domain hung.** A request from a
container to a name that points at this machine timed out, while the
same request from the machine itself answered. Docker hands that traffic
to the host rather than straight to the proxy, and on an instance whose
firewall Cubeship manages, the host refused it without a word.

The firewall block Cubeship keeps now lets containers reach the ports
this machine publishes — 80, 443 and every exposed database or object
store — by the machine's own address. Nothing else on the host opens to
containers; SSH stays closed to them. **Nothing to do on your side**: the
daemon rewrites the block when it starts, so the upgrade applies it.

### Changed

**A project a template install creates wears the template's icon** — the
icon of the release installed. A project that already existed keeps its
own picture, and an icon that cannot be fetched never fails the install.

## 0.7.0-rc.3 — 2026-09-13

*Prerelease.*

Templates load on an instance that serves the catalog itself, instead of timing out.

### Fixed

**Templates could not be listed when the catalog ran on the same
machine.** Opening Templates answered "the template catalog could not be
reached" after twenty seconds. From inside the machine, a name that
points back at it leaves for the machine's own public address, and many
hosts never deliver that traffic back to themselves — so the request
hung until it gave up.

A request for a name that resolves to this instance — its public
address, or what its own domain resolves to — now goes straight to its
proxy over the internal network, with the same name and the same
certificate check as from outside. Nothing to configure, and no private
DNS. Every other address still goes out over the internet.

## 0.7.0-rc.2 — 2026-09-13

*Prerelease.*

A switch to be offered beta versions — betas and release candidates — from the update button, and prereleases ordered the way semver says.

### Added

**Beta versions, if you ask for them.** Under **Settings → Updates**,
**Receive beta versions** makes the update button offer betas and
release candidates — `0.8.0-beta.1`, `0.8.0-rc.1` — as well as stable
releases. It is off by default.

- **It changes what the button offers and nothing else.** Automatic
  updates stay on stable releases with it on: a beta is something you
  choose to try, not something that should arrive on its own at three in
  the morning.
- **Turning it off never goes back a version.** An instance on a beta
  stays where it is until a stable release above it comes out, and is
  offered that one.

Until now the only way onto a candidate was asking for it by name, which
is still how an instance on 0.6.0 gets here.

### Fixed

**Prereleases are ordered part by part.** What followed the hyphen was
compared as text, so `rc.10` came before `rc.2`. Numbers are numbers
now, and `beta.3` comes before `rc.1`.

## 0.7.0-rc.1 — 2026-09-13

*Prerelease.*

Templates — install Umami, n8n or Grafana from the dashboard in one form, then update or uninstall it later. Plus a firewall fix for exposed databases and object stores, and cards that show what everything is using.

### Added

**Templates.** A template is a GitHub repository with a `template.yaml`
describing apps, the databases and object stores they need, and the
questions an installer has to answer. The catalog at
[cubeship.dev/templates](https://cubeship.dev/templates) indexes their
releases, and **Templates** in the dashboard reads it.

- **Installing is one form.** Pick a project and an environment — new or
  existing — answer the template's questions, and the instance creates
  every database, store and app it declares, wires them together and
  deploys them. Secrets it generated are shown once, at the end.
- **A failed step undoes the whole install.** Nothing half-created is
  left behind to find and delete by hand.
- **Installed lists what you installed** and says when the catalog has a
  newer release. Updating shows what will be created, changed and kept
  before anything happens, asks only the questions the new release
  added, and **never deletes**: something the new release no longer
  declares stays where it is. If a step fails, the apps go back to how
  they were and are redeployed.
- **Uninstalling keeps the data by default.** The apps go, and so do a
  project and an environment the install created if they are left empty.
  Its databases and object stores stay unless you untick the box and
  type the template's name.

Only an admin installs, updates or uninstalls. The same is in the CLI —
`cubeship template install`, `installed`, `update` and `uninstall` — and
in the MCP tools.

Opening Templates is the only thing that reaches cubeship.dev: a plain
GET for the catalog and one for the template's file on GitHub, with
nothing about your instance in either. `CUBESHIP_CATALOG_URL` points it
somewhere else.

**Cards show what everything is using.** Projects, apps, databases and
object stores are cards, each with a CPU bar and a memory bar along the
bottom. An app's bars are a share of its own limit, or of the machine
when it has none; a project's add up every app in it against the
machine. Anything not running keeps its bars, at 0. Databases and
object stores wear their engine's or provider's mark.

**An app's addresses can be copied and opened from its page.** A
Networking section under Monitoring lists its internal address — copied
as a URL, port included — and every public domain, each with a copy
button and one that opens it in a new tab.

### Fixed

**An exposed database or object store could be unreachable, or open
more than it should.** Exposing one published a host port and never
told the firewall. On an instance whose ufw had adopted Docker with
incoming denied, the port was refused; and a rule written by hand could
only name the port inside the container, which every database of that
engine shares — opening one Postgres opened all of them.

Cubeship now keeps its own block in ufw's `after.rules`, matching each
exposed port by the number it was published on, and rewrites it
whenever something is exposed, moved or unexposed. **Nothing to do on
your side**: the daemon writes it on start. The Firewall screen says
what admits each published port — a ufw rule, or that block — and no
longer offers an exposed port as one to adopt, since ticking it wrote
exactly the rule that opened every database of its engine.

**Exposing a store on the port it already has** answered "port taken"
instead of doing nothing.

## 0.6.0 — 2026-09-11

Databases can be backed up on a schedule, the instance can back itself up including its own database, and the dashboard was rebuilt around where you are rather than what page you opened. Plus the fix for an app pinned to a tag redeploying on every push.

### Breaking

**The description is gone from projects, environments and apps, and the
text goes with it.** Upgrading drops the column. Nothing copies it
anywhere first, and the migration's Down adds an empty column back —
whatever was typed is not recoverable. **If you have descriptions you
want to keep, read them before upgrading.**

It was optional, asked for once at creation, read by whoever already
knew what the thing was, and shown on exactly one screen. Behind it sat
a settings section per level whose only reason to exist was editing it.
What names something here is its slug, which is in the URL, in the
container's name and in the registry path.

**`PATCH /projects/{slug}` and `PATCH /projects/{slug}/environments/{env}`
are gone with it.** Neither had anything left to change — slugs are
fixed and variables have their own endpoints — and an endpoint that can
change nothing promises a caller that something can. An app keeps its
PATCH, which still carries its source, its ceiling and where it runs.

### Added

**Your databases can be backed up.** Postgres, MySQL, MariaDB and
MongoDB, each dumped the way its own tools do it, with a Backups tab
inside the database's settings.

- **On a schedule, or now.** Pick a time of day and a timezone — 03:00
  on a server's clock is not the middle of anybody's night — and say how
  many to keep. Or press the button.
- **Somewhere that is not this machine.** Point it at an object store
  you have linked and the dump streams straight into the bucket without
  ever landing on this box's disk, so a database larger than the disk
  under it is still a backup. With nothing linked it writes locally,
  which works the minute the instance is installed and is not a backup —
  every screen showing one says so.
- **Restored from the same screen**, into the database it came from,
  after typing that database's name.
- **They outlive the database.** Deleting one keeps its backups, which
  is the moment they matter most. Those live at **Backups** under
  Platform, and can be downloaded.

Retention counts only the dumps that worked. A week of failures will
never push out the last good one, and a failed run is kept — it is the
evidence that a schedule is not working.

**Redis is not in the list, on purpose.** It is a cache and a queue on a
box this size, it already survives a restart on its own, and what a
nightly copy of one would be restored *to* is a question with no good
answer. Its page offers no Backups tab rather than an empty one.

**The instance can back itself up, database included.** Under
**Settings → Backups**: Cubeship's own Postgres — every account,
project, app, credential and attachment — plus the Let's Encrypt store
and the project pictures, in one archive on the same schedule machinery
the databases use. It is the most valuable database on the box and it is
not a datastore, which is why backups was built as a module above them
rather than inside.

**Backups answers "am I covered" instead of listing every dump.** The
instance-wide screen was every dump on the box, newest first, which is a
log — and built from the dumps, it could not report the database nobody
has ever backed up, which is the row that matters most. It is one row
per database now, worst first: never backed up, failing, on this
machine, protected. An instance with nothing wrong is one you can stop
reading after a glance.

It reports two facts rather than one, because neither is the other's
opposite: whether there is something to restore that is not on this
machine, and whether the last attempt failed. Last week's dump can sit
safely in a bucket while every night since has failed.

**An app has an internal address.** `cubeship-<project>-<env>-<app>`,
shown on the app's Network tab, is where another app on this instance
reaches it — from any machine in the cluster, to whichever copies are
running, on the app's own port.

A public name cannot do that job from inside the box: the request leaves
it for a DNS record pointing back at it, and a host that does not
hairpin its own NAT answers nothing at all, which reads as the other app
being down. The name is a network alias rather than the container's own,
so it survives a deploy — **an app has to be deployed once after
upgrading before it answers to it.**

**An account says who is behind it.** A display name, an email, a face,
and the username itself. Everywhere a person is named the dashboard now
shows the display name and falls back to the username; the Users table
keeps both, because there the address is the thing you act on.

A username is editable here and nowhere else in this product. Sessions
and API keys are held by id and survive it. The one thing it breaks is
yours: `docker login` sends the username with the key, so a push keeps
being refused until you log in again.

Nothing on this instance sends mail, and the email field says so where
it is typed.

**An admin can block an account, change its role, or issue it a new
password.** Deleting was the only answer before, and it is the wrong one
for everything but somebody leaving for good.

- **Blocking revokes nothing.** The account keeps its password, its keys
  and its sessions, and all three are refused at the door instead — so
  unblocking puts somebody back exactly where they were.
- **A new password leaves the API keys alone**, which is what separates
  it from revoking credentials. A forgotten password is not a lost
  laptop, and this box sends no mail, so an account that had forgotten
  one had nothing to try.
- The last admin cannot be deleted, demoted or blocked, counted inside
  the transaction so two admins cannot take each other's role in the
  same moment.

**A project can wear a picture**, chosen on its settings screen, with a
mark until it does. The dashboard crops and scales before sending; the
daemon bounds it at 512 KiB and decides the type from the bytes rather
than the header. PNG, JPEG and WebP — no SVG, which is a document with
scripting in it.

**Two new palettes, and a face for every one of them.** `blue` is navy
rather than the default with the accent nudged. **`helix` is the first
light one** — Mono the other way up, black ink on paper, for a console
read in daylight. The faces are nine different people rather than one in
nine colours.

**`cubeship version` answers.** It never did — `-X main.version=` was
writing to a constant, so every CLI released so far reports `dev`. Yours
will say the real number once you upgrade it.

### Changed

**The dashboard tells you where you are.** Thirteen screens rendered
their own "back to the thing above" link and the rest rendered nothing.
There is one rail across the top of every screen now, built from the
URL, and it replaced the page header under it rather than sitting above
one — almost every title it drew was either the section the sidebar
already highlights or the last word of the path.

- **A crumb with siblings is a menu.** Opening `production` to land in
  `staging` is the trip back through two screens you no longer take.
  Projects, environments, apps, databases, stores, buckets, zones,
  registries and DNS providers all switch from the rail.
- **A screen's tabs hang off the rail as a strip of their own**, like a
  browser's: the open one is the page, the closed ones sit on the strip.
- **A screen's one action lives in the rail** — Add user, Add domain,
  New project — instead of in a card above the list it acts on.

**A settings screen that configures more than one thing is tabs**, and
the test is whether they answer different questions. An app's five
sections became Network, Source, Resources and Danger — how much machine
an app gets is where it runs, what it may take and whether it picks its
own count, and reading one without the others tells you a third of it.
Databases, object stores and the instance's own settings split the same
way. **Danger is a tab**, because at the foot of whichever tab happened
to be open a delete is behind nothing.

**Every listing is a table with a filter**, from one place: projects,
databases, object storage and its buckets and files, credentials,
certificates, servers, DNS providers, registries, users, API keys and an
app's domains. It was written out beside a table four times before this
and came out four ways.

**The project cards carry a picture, a name and a ring.** The
environments were badges, which put `production` on every card on the
screen; the app count had a line of its own; and the states were a row
of lamps with numbers beside them, which is a legend you read rather
than a picture you glance at. The ring is a thin donut with the count in
the middle — whole and green is everything, a bite out of it is the
thing to open.

**Environments are tabs in the rail** rather than a switcher inside the
page. An environment is a level of the hierarchy the crumbs already
spell out, and switching one changes the address.

**Certificates are one table**, not Issued above Waiting — what you come
to find out is whether a name is served, and that is a column. Each
reason is a row action on the rows that have one, rather than a
paragraph in every cell.

**The command palette split in two.** `Cmd+K` finds things, `Cmd+Shift+P`
runs commands, and `>` switches between them. "New database" and the
database called `pg` sat beside each other under `d`, and picking the
wrong one is either a form you did not want or a screen you did not
want. Nothing irreversible is reachable from either: every command opens
a form.

**A store's bucket can be moved or cleared from its settings.** Until
now the only way out of a store pinned to one bucket was linking it
again. Clearing the field hands the question back to the endpoint. The
field is offered for every provider, with a line saying what pinning
gives up, rather than only where a per-bucket key is the usual choice.

**"Storage" in the sidebar is "Object storage"**, and **"Instance" is
"Settings"**. The routes are unchanged.

**The databases list drops the exposed port.** Three columns: what it is
called, what it runs, whether it is up. The port is on the database's own
page under External access, which is the only place that can also say
there is no TLS in front of it.

### Fixed

**An app pinned to a tag was redeployed by every push.** This is the one
worth upgrading for. Pinning an app to `v1.0` is how deploy-on-push is
turned off, and it silently did nothing: a push of any tag to this
instance's registry deployed the app anyway. Every screen also reported
such an app as following the registry.

The tag was missing from the query behind every read of an app, so the
daemon saw an empty one everywhere and read that as "not pinned".
Nothing needs changing on your side — a pinned app stops moving on the
next push.

**A backup into this instance's own MinIO was reported as protected.**
"Off the machine" was "has an object store", and a managed store is a
container this instance runs with its objects in a bind mount under the
data directory — the same disk as the database and as a local dump.
Nothing about it looked broken: the dump succeeded, the object was
written, and the row read exactly like one that went to another
continent. It is recorded per dump now, so a store deleted later cannot
take the answer with it.

**Logs are readable again when a program writes colour.** Lines from
anything that colours its output arrived as `[2m2026-…[0m [32m INFO[0m`
— the escape codes printed instead of applied, which made the one field
you were trying to read the least readable thing on screen. They are
rendered now, in the colours the program chose. Filtering and
downloading see the text with the codes taken out, so a search matches
what you can see and a saved file opens in an editor.

**Cubeship's own registry can be tidied up like any other.** Deleting a
tag or a repository was offered for every registry you connect and not
for the one this instance runs, which is the one filling your disk.
There is a **Reclaim disk** button beside it: deleting a tag unlinks the
manifest and leaves the layers, so without a garbage collection pass you
clear a repository and watch the disk not move. No repository anywhere
showed a size, either.

**Switching environments blanked the grid.** The page took itself down
for a round trip between two lists that are mostly the same apps.

**The users list answered without the face or the name** it exists to
show — it was building a three-field response of its own.

**A table no longer takes the screen down** when a field arrives as
undefined, which is the shape of an ordinary disagreement between a
daemon and a dashboard a version apart. It keeps drawing its skeleton.

**The certificates page crashed while it loaded**, and a lone crumb no
longer repeats the title under it.

### Security

**A region can no longer point an endpoint somewhere else.** The region
typed when connecting an ECR registry or an S3, Spaces or R2 store is
built into a hostname this daemon then connects to, and it was accepted
as-is. A value carrying a `#`, a `/`, a `:` or an `@` could end that
hostname early and send the request — signed, for ECR — to a server
nobody chose. Only an admin could set one. Regions and account ids are
now checked when they are typed.

The Docker SDK moves to 28.5.2 and containerd to 2.3.5. The advisories
those close are in the Docker daemon rather than in Cubeship, which
embeds only the client — the Docker on your machine is what carries a
fix for them, and `get.docker.com` installs a current one.

Cubeship is under **Apache-2.0** as of this release, with the name
reserved (`TRADEMARK.md`) and a `SECURITY.md` that says what is not a
vulnerability as carefully as what is. There is no telemetry: the only
thing the daemon sends on its own is a plain GET asking GitHub which
release is newest.

## 0.5.1 — 2026-09-11

Deploying an app from this instance's own registry failed on a name Docker could not look up — the pull was sent to an address only the daemon can reach.

### Fixed

**An app that pulls from this instance's own registry could not be
deployed.** It failed with:

```
lookup cubeship-registry on 127.0.0.53:53: server misbehaving
```

which reads as a broken registry and is nothing of the kind. The
reference the pull was given began with the registry's *container name*,
and a container name is only resolvable by other containers on the same
Docker network. The pull is performed by Docker itself, which is not on
that network — so the name was never going to resolve, and the registry
it names was up and answering the whole time.

Pulls now use the registry's address on the host, which is published
either way and works whatever else is running. Nothing to change: push
and deploy as before.

It only ever affected apps whose image is **pulled**. An app Cubeship
builds — from a Dockerfile or with Railpack — has its image handed
straight to Docker with no pull at all, so an instance that only builds
never met this.

## 0.5.0 — 2026-09-11

An app that runs a published image is now configured by choosing a registry, an image and a tag, each from a list — and pinning a tag is how deploy-on-push is turned off.

### Changed

**Where a Docker image comes from is three choices, not two cards.** It
was "Cubeship's registry" or "Another registry", above a box you typed a
reference into. The second was never one thing: it is however many
registries you have connected, and picking it told you nothing about
which.

Now you pick the registry, then the image, then the tag — and each is a
list this instance fetches:

- **The registry** is Cubeship's own, every registry you have connected,
  and Docker Hub. Docker Hub is there whether or not you have connected
  it, because a public image needs no login.
- **The image** comes from that registry's own catalogue. Docker Hub has
  no public catalogue, so you type the image there and the tag is still
  listed; a registry that will not list what it holds behaves the same
  way.
- **The tag** is newest first, and the newest is already chosen.

On Cubeship's own registry there is no image to choose: it is this app's
own path, shown and locked. A push is matched to an app by its
reference, so pointing one app at another's path would deploy the wrong
app every time somebody pushed.

### Added

**An app can be pinned to a tag**, and that is how deploy-on-push is
turned off.

Until now an app on this instance's registry deployed on every push and
there was no way to say otherwise. Choose a tag and it runs that one,
deployed when you ask for it — a push is ignored from then on, including
a push of the very tag it is pinned to. An app pinned to `v1.0` is one
somebody decided should run `v1.0`, and moving it because a notification
arrived would undo that decision with nobody watching.

The switch and the tag are the same setting seen twice, which is why the
screen hides one when you use the other: an app cannot both follow every
push and be pinned, so there is no way to set them to disagree.

**Nothing changes for an app you already have.** Every app starts
unpinned, which is exactly what it does today: a push to this instance's
registry deploys it, and an app pulling from anywhere else runs
`latest`.

For the API and the CLI, `tag` joins `image` on `POST /apps` and `PATCH
/apps/{ref}`, `autodeploy` is reported beside it, and `cubeship app
create` gained `--tag`. `cubeship app deploy --tag` is unchanged and
still wins over whatever is pinned — what you asked for beats what was
configured, and the deployment history records the tag that actually
ran.

## 0.4.3 — 2026-09-11

On the Certificates screen, the reason a certificate had not been issued was printed as line noise instead of as a sentence.

### Fixed

**"What Traefik says" was unreadable.** A name waiting on a certificate
showed something like `r[90m2026-09-10T23:57:50Z[0m [31mERR[0m` where
the reason was meant to be, and that field is the only place an ACME
refusal is written down at all.

Two things overlapped. Traefik colours its own log whether or not
anything is reading it as a terminal, and the escape byte was being
dropped on its own — which leaves the rest of each colour code sitting
in the text. And the binary header Docker puts in front of each chunk of
a log was being removed by discarding its unprintable bytes, which the
length byte is not for any line of ordinary length.

Both come off properly now, so the field reads as what it is:
`2026-09-10T23:57:50Z ERR Unable to obtain ACME certificate for
domains…`, timestamp included.

## 0.4.2 — 2026-09-10

A firewall rule for an exposed database admitted nothing — it was written for the port you published, and Docker has already changed that number by the time the firewall sees the packet.

### Fixed

**A firewall rule for an exposed database or object store never admitted
anything.** If you published a Postgres on 15000 and allowed 15000, the
rule was written, listed, and matched no packet ever: rules about
forwarded traffic are consulted after Docker has rewritten the
destination, so what arrives is addressed to 5432 and a rule naming
15000 cannot see it.

It survived this long because the only ports that get rewritten are the
ones you open yourself. Everything Cubeship publishes for itself —
Traefik's 80 and 443, the dashboard's 3000 — is the same number inside
and out, so the feature worked for every port the product opens and for
none of yours.

Rules are now written for the port the container is actually on, and the
Firewall screen shows the translation: `15000/tcp → 5432`.

**If you have already put your published ports behind the firewall**,
the rules you wrote for a database are the old, inert kind. After
updating, that port shows as admitted by nothing — add the rule again,
and delete the old one, which is doing nothing and only makes the list
harder to read.

**A certificate that failed to issue was never asked for again.**
Traefik asks a certificate authority only when its configuration
changes, so a name whose first attempt failed — a DNS record written a
minute too late, an hour when Let's Encrypt could not reach your
nameservers — stayed without one until something unrelated happened to
change the routes. On a settled instance that could be never, and the
only way out was redeploying an app with nothing wrong with it.

This instance now asks again every half hour, for as long as any name is
waiting, so a name that starts resolving here gets its certificate
without you doing anything. Only names Traefik is actually waiting on:
an instance with no domain, or an app not redeployed since the name was
added, are still things for you to do, and asking would not move either.

### Added

**Postgres 18** is on the list of versions a new database can run.

It needed more than the number. Postgres 18's image moved where it keeps
its data and declared its volume somewhere else, so a database created
on it would have come up, worked, and kept everything in a place this
instance does not know about — in no backup of the data directory, and
thrown away the next time the container was replaced. Cubeship now says
where the data goes rather than letting the image decide.

Existing databases are untouched: a datastore's version is fixed for its
life, which is what keeps a data directory readable by the server that
wrote it.

### Changed

**Creating an account hands back a password, not an API key.** The
account it used to make could not sign in anywhere: a key is what the
CLI and MCP clients carry, the dashboard wants a password, and nothing
here lets a new person set a first one — there is no invite mail and no
reset flow. So you created somebody an account, handed them a
credential, and it opened nothing you had given them the address of.

The password is shown once — this instance keeps only its hash — and
whoever it belongs to changes it from their own account screen. API keys
stay self-service, made by the person who wants one.

**This changes the response**, so anything scripted against it needs a
look: `POST /users` returns `password` where it returned `api_key`, and
takes an optional `password` of your own. `cubeship user create` prints
the password and gained `--password`.

**The `revoke_api_key` tool told agents the last key cannot be
revoked.** It can, deliberately — a leaked key has to be able to go
now — and the description says so, along with what revoking it costs.

## 0.4.1 — 2026-09-10

Users moved to Platform, where the instance's facts live, and the screen is built out of the components every other listing here uses.

### Changed

**Users is in the sidebar under Platform**, beside Credentials, rather
than tucked into your own settings. Who can reach this instance is the
same kind of fact as which registry it pulls from — your password and
the colours you see are the other thing, and those stay under your name.

A member who opens it is told it is an admin's to see, rather than shown
an empty table.

### Fixed

**The screen is put together like the rest of the dashboard.** It had a
table of its own and a bare dropdown: the table did not sort or show
that it was loading, and the dropdown was a different height from the
field beside it. Both are the shared components now.

**Adding somebody is above the table**, because that is what brings
anybody to the screen. The table answers "who is there", and you already
know when it is only you.

## 0.4.0 — 2026-09-10

A screen for who can reach this instance, seven palettes to see it in, and your own settings moved out from under the instance's.

### Who has access

**Account → Users**, for an admin. Add someone, see who holds a way in,
revoke every credential one account has, or remove it entirely.

The API for all of this has been there since the first release with
nothing in the dashboard reaching it — this is the screen.

Adding somebody hands back an **API key, shown once**, and no password.
The key is what gets them in; the password is theirs to set, and this
instance only ever keeps hashes of either.

Two things it refuses, and it says so before you click rather than
after: the account you are signed in as, and the last admin. Nothing in
the API can make an admin without one.

It is an admin's screen including the reading. What the list says is who
can get in, which is not something a member needs and is exactly what
somebody probing would want.

### Seven palettes

**Account → Appearance.** Cubeship, Mono, Hacker, Red, Orange, Pink,
Purple.

Every one of them is dark, and that is a decision: this is a console for
a machine, read beside a terminal, and a light one would be the only
screen on that desk that is. Each changes **colour and nothing else** —
the layout, the type and the square corners are the product.

Your choice is saved to your account rather than to the browser, so it
follows you to another machine instead of leaving one person with two
different-looking instances.

### Your settings are yours

`/account` is tabs now — how you sign in, what it looks like to you, and
who else can get in — and it is reached from the menu under your name
rather than from the sidebar. That sidebar section held one item, and
what is yours is not what the instance is made of.

**Settings in the sidebar is still the instance's**: its domain, its
contact address, when it updates itself. Nothing moved there.

## 0.3.3 — 2026-09-10

Automatic updates accept a timezone — the daemon's image had no timezone database, so every zone name was refused.

### Fixed

**Scheduling an automatic update refused every timezone.** Not an
unusual one: `America/Bahia`, `Europe/Lisbon`, any of them. The daemon
runs on an Alpine image, Alpine ships no timezone database, and nothing
in the daemon carried its own — so it could not recognise a single name
and said so about each one in turn.

The database is in the binary now, so it no longer matters what the
image underneath has. Set the hour and the timezone in **Settings →
Automatic updates** and it will hold.

It is also no longer quiet about falling back to UTC. That combination —
a name it could not load and a silent fallback — would have been an
instance updating itself at the wrong hour for as long as nobody
happened to be watching at the time.

## 0.3.2 — 2026-09-10

Updating twice works — the container that replaces the daemon was never being cleaned up, so the second update an instance ran failed on its own leftovers.

### Fixed

**An instance could update once.** The daemon is replaced by a throwaway
container, and that container was asked to delete itself with a flag
that never reached Docker — so the first update left it behind, and the
second failed on the name being taken.

It failed *between* the two halves: the dashboard had already been
replaced and the daemon had not. So the instance looked updated, kept
saying it was on the release before, and went on offering the one it was
already showing you.

If you are seeing that, the update below fixes it for good — but this
instance still has the old updater in the way, so clear it first:

```
docker rm cubeship-daemon-updater
```

Then press Update. From this release on, the name is cleared before each
update whatever happened last time: a machine rebooted mid-update should
not be a machine that can never update again.

**The dashboard could come back a version behind.** The daemon's
environment is carried over when it is replaced, and one variable in it
names the dashboard's image — so a daemon that came back still pointing
at the old one would have put the old dashboard back on its next
restart. An update that looks done and undoes itself on a reboot.

### Changed

The link to the repository is in one place now, beside your name in the
sidebar, and carries the project's star count.

## 0.3.1 — 2026-09-10

The release notes are reachable from the user menu now, their footer button sits inside the panel again, and there is a link to the repository.

### Fixed

**The button in this dialog's footer** was sitting outside the panel it
belongs to — the footer carries margins that assume the popup keeps its
padding, and this dialog takes it off to hold the header and footer
still while the notes scroll.

### Added

**Release notes are in the user menu.** They used to appear once after
an upgrade and then be gone. Opened from the menu they are the history —
every release up to the one this instance runs — and closing marks
nothing, because reading them again is not an event.

The menu also carries the account and a link to the repository, and the
sidebar's foot has that link as an icon: the one thing in this dashboard
that is not part of your instance.

## 0.3.0 — 2026-09-10

Three fixes to this dialog — the button that dismisses it now actually does, it no longer scrolls sideways, and the header and footer stay put.

### The notes you are reading

**"Got it" marks them read.** It did not: the button closed the dialog
and told the daemon nothing, so the next reload showed the same notes
again and only the `×` ever ended them. It is one path now, and the
instance is told before the dialog goes — a reload a moment later was
cancelling the request that said so.

**No more sideways scroll.** A release note is full of commands, and one
long enough to overflow was moving the whole dialog rather than itself:
the prose went with it, and the button slid out from under the pointer.
A command block now scrolls on its own.

**The header and the button stay put** while the notes scroll between
them. On a long release the button was below the fold, which is a poor
place for the only thing that dismisses something.

The dialog that offers an update got all three, because it is the same
dialog showing the same kind of text.

### Updating to this one

This is the first release you can install from the dashboard — 0.2.0 is
what put the button there. If this instance is on 0.2.0, the dialog
offering 0.3.0 is the whole of it.

## 0.2.0 — 2026-09-10

An instance updates itself — every machine in the cluster, on a schedule if you want one — and the CLI is a download rather than a build.

### Updating from the dashboard

When a release is out, the dashboard says so and offers to install it.
Taking the offer replaces the daemon, the dashboard, and **every other
machine in this cluster**. Your apps and databases keep running
throughout: what is being replaced is the thing that decides what runs,
not the things it decided.

Say no and it stays out of the way, with a button in the corner for
whenever you do want it — an update offered once and never mentioned
again is one nobody takes.

**Nothing on the instance can be changed while an update runs.** Every
write answers 503 until it finishes, including from a browser that has
just reloaded: the moment worth protecting is exactly the one where the
daemon has restarted underneath somebody. The dashboard covers itself
and shows what step it is on, and comes back on its own — it goes quiet
for a few seconds while the daemon is replaced, which is the one part
nothing can report from the inside.

### Updating on a schedule

**Settings → Automatic updates.** Pick a time of day and a timezone, and
the instance keeps itself current.

A time rather than an interval, because what you are choosing is when
the instance may be briefly unusable — and "every 24 hours from whenever
you turned it on" is not something anybody can plan around. The timezone
is the half that makes the number mean anything: 03:00 on a server's
clock is not the middle of your night.

Only stable releases. An instance left to update itself should not
wander onto a release candidate at three in the morning.

### The other machines

They go first, and they have to: once the control plane restarts it can
tell nobody anything, so a cluster updated the other way round is one
where every worker is a release behind and nothing is coming to move
them.

A machine that is off is carried on without. One box being unreachable
must not freeze the whole cluster, and it gets the new version whenever
it comes back.

### The CLI is a download now

Every release carries `cubeship` as a static binary for macOS and Linux,
Intel and ARM, with checksums beside them. Before this, installing it
started with cloning the repository and having Go.

```
curl -fsSL https://github.com/cubeshipd/cubeship/releases/latest/download/cubeship_0.2.0_darwin_arm64.tar.gz | tar -xz
sudo mv cubeship /usr/local/bin/
```

`cubeship app create` can also set the image an external app pulls,
which it could not before — so creating one no longer needs the
dashboard.

### Updating **to** this release

The button does not exist on 0.1.0, so this one time it is the installer
again:

```
curl -fsSL https://raw.githubusercontent.com/cubeshipd/cubeship/master/install.sh | sh
```

From here on it is a button, or a time of day.

### Underneath

Releases are built on a machine of each architecture rather than one
emulating the other, which took the pipeline from most of an hour to
about three minutes. Nothing about an installed instance changes; the
arm64 images are simply no longer built through QEMU.

## 0.1.0 — 2026-09-10

The first release. One command installs a PaaS on one VPS, and a second machine joins it.

### Installing

One command puts Cubeship on a Debian or Ubuntu box: it installs Docker
if it is missing, pulls two images, and runs the daemon. There is nothing
else to host anywhere, and an upgrade is a pull.

The instance is reached at `http://<ip>:3000` before it has a domain, and
claiming it takes a token the installer prints — so being first to the
page is not enough to become the admin of somebody's machine.

`uninstall.sh` is the counterpart, and its default is not the destructive
one: it removes the containers and leaves the data, because somebody
removing the software is not thereby asking to lose their database.

### Apps

An app is created empty and deploys anyway. It has no domain until you
give it one, which is a normal state — a worker or a queue consumer has
no business answering on the internet.

Four sources, which the dashboard shows as the two things an app can be:
something this instance builds, or something someone else already built.

- **Pushed here.** `docker push` to the instance's own registry deploys it.
- **From another registry.** Docker Hub, ECR, Spaces, or anything that
  speaks S3-style auth. A deploy is something you ask for.
- **Built from a Dockerfile** in a Git repository.
- **Built with Railpack** from a repository with no Dockerfile at all.

Both build sources deploy on push once the GitHub App is connected.

Deploys are detached: a client that times out stops waiting, not
deploying. A build's output is written to the deployment row while it
runs, so a build is watchable rather than a blank wait, and the last
thing it printed is the thing kept when the log is trimmed.

Every name is served over HTTPS with a certificate this instance gets
itself. On a default install the address is an sslip.io name, and every
name under one already resolves — so an app has a working address the
moment you add one.

### More than one machine

An instance is a control plane and any number of workers. A worker runs
the same image in a mode where it decides nothing, publishes no port, and
dials home every ten seconds — so a machine behind a firewall joins with
one outbound connection and nothing has to be opened to reach it.

`cubeship server add <name>` prints the command to run on the new box,
with the address and the credential already in it.

The machines share Docker's own overlay network, **encrypted**: all app
traffic crosses it, and TLS ends at this instance's proxy, so what would
otherwise go between boxes is plain HTTP with its cookies in it.

Every name arrives at the control plane, whichever machine runs the app.
One DNS record, one certificate store, and moving an app between machines
touches neither.

An app runs as many copies as you ask for, spread over the machines it is
on — or, with one switch, over every machine there is and any that joins
later. Scaling takes effect at once in both directions, on any machine.

### Limits and autoscaling

An app, a database and a managed object store can each be capped: how
much CPU one container may use, and how much memory it may hold. Changing
a limit does not restart anything — it is the one part of a container
Docker can change while it runs.

An app's replica count can be handed to the instance, which works from
the average CPU across its copies. It is damped so it settles rather than
oscillates, and a ceiling is required: without one, a loop of requests is
a loop of replicas.

### Databases and buckets

Postgres, MySQL, MariaDB, Redis and MongoDB, provisioned in one request
and attached to any app in any project — a database belongs to the
instance, because on one box the common shape is a single Postgres
serving several small apps.

Object storage is either a MinIO this instance runs or an S3 endpoint it
holds the keys to, and everything above the connection is one screen for
both. An attached app receives the connection details in its own
environment, so nothing has to hold the credential itself.

### What the instance can tell you

What every container is using, what the machine underneath them is doing,
which certificates exist and why one is missing, and what the host's
firewall admits — including the ports Docker publishes around it, which
is the thing a plain `ufw status` would not show.

### The API, the CLI and MCP

Everything the dashboard does is an HTTP API with an OpenAPI document at
`/openapi.json` and a reference at `/docs`. `cubeship` is the CLI over the
same API, and `/mcp` is the same surface for an agent — with two lines
deliberately not crossed: no tool reads a secret, and no tool sets a
container's ceiling.
