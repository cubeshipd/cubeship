# Changelog

Every release of Cubeship, newest first.

<!-- Generated from internal/release/notes by cmd/changelog. Edit a note
     there and run `make changelog`; editing this file is editing the
     copy rather than the thing. -->

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
