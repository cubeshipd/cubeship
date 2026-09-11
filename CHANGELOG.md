# Changelog

Every release of Cubeship, newest first.

<!-- Generated from internal/release/notes by cmd/changelog. Edit a note
     there and run `make changelog`; editing this file is editing the
     copy rather than the thing. -->

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
