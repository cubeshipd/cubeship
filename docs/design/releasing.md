# Releasing, and updating itself

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

## Releasing

A tag, and nothing else. `v0.2.0` is a stable release and `v0.2.0-rc.1`
is a prerelease, because semver says a version with a hyphen in it is
one — there is no second place to say so and disagree.
`.github/workflows/release.yml` builds both images for both
architectures, attaches a provenance attestation saying which commit
they came from, and creates the GitHub release.

**Each architecture is built on a machine of that architecture** — amd64
on `ubuntu-latest`, arm64 on `ubuntu-24.04-arm` — and the two are named
by one manifest afterwards. Building both on one runner means one of
them under QEMU, and a Go toolchain emulated instruction by instruction
is the difference between a release taking minutes and taking most of an
hour, on the one pipeline nobody can shorten by skipping. Each arch is
pushed **by digest with no tag**: a tag naming one architecture is a tag
that is wrong for half the machines pulling it, for as long as the other
build takes.

**`latest` moves only for a stable release.** Somebody who installed
without naming a version must never be upgraded onto a release
candidate by a tag they did not ask for.

### The notes are a product artifact

They are written by hand in `internal/release/notes/<version>.md`, one
file per release, and they are the same text three ways: the GitHub
release's body, `CHANGELOG.md` in the repository, and the dialog the
dashboard shows after an upgrade. **The release workflow refuses a tag
that has no note**, because a release nobody wrote notes for is one
whose dialog is blank.

Generated changelogs were the obvious alternative and are the wrong
shape here. Conventional commits exist to be parsed, and this
repository's commit subjects already say what changed in words somebody
would read — `Cap what an app may take from its machine` against
`feat(app): add container limits`. Turning those into a bulleted list of
commits would be *less* than what is there, and it would be less in the
one place it becomes product: nobody reads `chore(deps): bump x` in a
dialog.

One file per release rather than one document with headings in it: a
parser that finds a release by heading breaks the first time somebody
writes a heading differently, and this is a file a person edits by hand
every few weeks. `CHANGELOG.md` is **generated** from them by
`cmd/changelog`, and `make check` fails when it is stale — two answers
to what a release said is one to disagree with.

### The dialog

`internal/release` is what version this instance is and what changed in
it. **The notes are in the binary, not fetched.** Cubeship runs on
somebody's own VPS, which may be behind a firewall and shares an address
with whoever else the provider put on it — so a dialog that asks GitHub
what changed is one that is sometimes empty and sometimes rate-limited,
which is worse than not having it. And there is nothing to fetch: the
notes for the version running are known when it is built.

What that buys is the property that keeps it honest: **an instance can
only ever show notes up to the version it is on.** A build carrying
notes for a version ahead of it — which is what a release branch looks
like mid-flight — would otherwise advertise something nobody can use.

`release_seen` is **per person, not per instance**: two admins on one
box should each read the notes once, rather than whichever opened the
dashboard first taking the notice away from the other. It is its own
table because a column on `users` would make the module that sits at the
bottom and knows about nothing else carry a fact about the changelog.
And it only ever moves forward — a second tab, or a request that arrives
late, would otherwise write an older version over a newer one and show
the same notes again.

A build with **no version stamped on it** — `make dev`, and every test —
has nothing to say changed. The history is still readable; the dialog
stays away.

The dashboard renders the notes with a small hand-written Markdown
subset rather than a library, and the reason is what the input is: these
files ship inside the daemon's own binary and are written by whoever
cuts the release. There is no user content, no HTML to sanitize and no
long tail of syntax to support. Anything it does not recognise comes out
as a paragraph, which is the right failure — a heading that renders as
text still reads.

## Updating itself

`internal/update` replaces this instance with a newer release, the
daemon included.

**The hard part is that the thing doing the work is what gets
replaced.** The daemon is a container; updating it means stopping that
container, which is the process running the code that asked. So the last
step is handed to a **throwaway container started from the new image**,
which outlives the daemon it replaces — the same door
`internal/platform/hostexec` opens for the firewall, used once more.
`cubeshipd -replace <name> -replace-image <ref>` is that mode, and it is
the one way this binary runs that is not a daemon.

Its options are **read back off the container being replaced**
(`dockerx.SpecOf`), not written in the updater. They were chosen by
whoever installed this — a port, a domain, a data directory somewhere
unusual — and a second copy of them in the binary is one that goes stale
the first time `install.sh` grows a flag.

**The updater's own name is cleared before it is made.** `AutoRemove`
was declared on `ContainerOpts` and never passed to the Engine, which
nothing noticed: the only other caller that used it removes its
container itself. The updater relied on it, so the first update left a
stopped `cubeship-daemon-updater` behind and the **second update an
instance ever ran failed on the name** — after the dashboard was
replaced and before the daemon was, which reads as an instance that
updated its own UI and then went on offering the release it is already
showing. The flag reaches the Engine now, and the name is cleared first
anyway: a machine rebooted mid-update would otherwise be an instance
that can never update again.

**`CUBESHIP_WEB_IMAGE` moves with the daemon.** Its environment is read
back off the container being replaced, which is right for every
variable in it and wrong for that one: it names the dashboard's image,
and a daemon that comes back still pointing at the old one puts the old
dashboard back the next time it starts — an update that looks done and
undoes itself on a reboot.

**The state is a file, not a row.** Everything else that remembers
something uses Postgres and this cannot: what it has to survive is the
daemon going away, which is exactly when nothing can answer a query.
`<data dir>/update.json` is read by the daemon that is going, the
throwaway updater, and the daemon that comes back — the data directory
being mounted at the same path inside and out is what makes that one
file. `setup-token` already works this way.

**Nothing on the instance can be changed while a run is going.**
`update.Guard` wraps the whole router and answers 503 to every write.
The lock is the server's rather than the screen's, and that is the
point: a browser that reloads forgets everything it knew, and the moment
worth protecting is the one where the daemon has restarted underneath
somebody. A run older than `StuckAfter` stops counting — an instance
locked for ever is worse than one that decides an update is over, and
the only way to leave one behind is the updater dying in the single
moment nothing is left to write the file.

**The other machines go first.** Once the control plane restarts it can
tell nobody anything, so a cluster updated the other way round is one
where every worker is a release behind and nothing is coming to move
them. A worker replaces itself the same way — its agent starts a
throwaway container — and **reports nothing back**, because answering
would mean surviving what it was told to do. What says it worked is the
version it reports on its next pass. One that does not come back is
carried on without: a box being off must not freeze the whole cluster.

**A version is named, never defaulted to "the newest".** A button that
says what it will install and a request that decides for itself are two
different promises, and the second one changes under somebody between
the screen rendering and the click.

**The timezone database is in the binary**, through a blank import of
`time/tzdata` in `cmd/cubeshipd`. The image is Alpine and Alpine ships
none, so `time.LoadLocation` found nothing and *every* zone name was
refused — an instance told to update at 03:00 in `America/Bahia` was
told that is not a timezone this machine knows. Embedded rather than
`apk add tzdata`, for the reason the fonts are vendored: a binary that
needs something from the image underneath it breaks the day somebody
builds it on a smaller base. No test can catch this from outside the
image — a developer's machine and the CI runner both have a system
database — which is why the import carries the reason it exists.

`update.Scheduler` is the automatic half: a **time of day**, not an
interval, because what is being chosen is when the instance may be
briefly unusable — and "every 24 hours from whenever you turned it on"
is not something anybody can plan around. It takes a timezone, because
03:00 on a server's clock is not the middle of anybody's night. Stable
releases only: an instance left to update itself must not wander onto a
release candidate.

**This is the one thing here that reaches the internet.** A build knows
every release up to its own and by definition nothing about the one
after — the changelog is carried so a dialog works behind a firewall,
and this cannot be, because the answer did not exist when the binary was
made. An instance that cannot ask answers `checked: false` rather than
claiming to be current.
