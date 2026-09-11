# Backups

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

`internal/backup` takes a copy of a database this instance runs, puts it
somewhere that is not this machine, and puts it back.

**Its own module, above `datastore` and `objectstore`**, the way
`certificates` sits above `app` and `settings`. It could have lived
inside `datastore` — a backup is a fact about a database — and what
decides it is a question about later: the most valuable database on this
box is Cubeship's own Postgres, which holds every user, project and app,
and is not a datastore at all. A module that could never reach it is the
wrong module.

What stays in `datastore` is the difference between engines, where it
already lives: `spec.dump` and `spec.restore` beside `env` and `cmd`.
This module decides when, where and for how long; that one knows
Postgres is `pg_dump` and MySQL is `mysqldump`.

**A logical dump, and only that.** It is the one kind that works the
same across the engines here, survives a major version changing under
it, and comes out as a single stream that can be sent somewhere else —
which is the only property that makes any of this a backup. Copying the
data directory is faster to restore and is pinned to the exact version
it came from, and taken while the engine runs it is simply corrupt.
Continuous archiving — WAL, binlog — is a different product: an archive,
a base backup underneath it, retention interlocked with both, and a
restore nobody rehearses.

**Redis has none, and that is a decision rather than a gap.** It is a
cache and a queue on a box like this, its own `--appendonly yes` already
survives a restart, and what a nightly copy of one would be restored
*to* is a question with no good answer. `Engine.CanBackUp` is what says
so, and the database's Backups tab is not offered at all where it
answers no — an empty tab explains nothing.

### Where one goes

**An object store, with this machine's own disk as the fallback.** A
dump beside the database it came from survives somebody dropping a table
and nothing else — not the disk, not the box, not the provider — so
`Backup.OffMachine` is a fact every screen showing one repeats. Local is
there because it works the minute the instance is installed, before
anybody has linked a bucket, and being unable to take a backup at all
until they have is worse.

**And "an object store" is not the same as "off the machine".** A
*managed* store is a MinIO container this instance runs, with its
objects in a bind mount under the data directory — the same disk as the
database, the same disk as a local dump. `OffMachine` was
`object_store_id IS NOT NULL` and therefore said yes to it, so an
instance backing up into its own MinIO was reported as protected, which
is the one thing the coverage report exists to prevent. The failure is
invisible from every side: the dump succeeds, the object is written, and
the row reads exactly like one that went to another continent.

`objectstore.LeavesThisMachine` is the question now, asked once when the
dump starts, and **the answer is a column**. Worked out on read it would
be a join through `ON DELETE SET NULL` — so a store deleted a month
later would take the answer with it, at exactly the moment somebody is
trying to find out what they still have. What was true when the dump
was taken stays true about it, which is the rule the engine and the
version already follow.

The schedule form says so where it is chosen: a managed store carries
the same warning the local disk does, and the destinations are listed
with the two that are on this machine sharing an icon.

The key is `cubeship/<database>/<timestamp>.dump`, built by `KeyFor` so
the row and the object cannot disagree about where it went.

**The stream never touches this machine's disk on the way out.** The
dump is an `io.Pipe` from the engine's own command straight into a
multipart upload with size `-1` — a database larger than the disk under
it is the ordinary case on a small VPS, and a temporary file would be
the one thing that makes a backup impossible exactly when it matters.
`dockerx.ExecStream` is what makes that possible: `Exec` buffers and
merges the streams for a log, and this one streams and keeps stderr
apart, which is the only place an engine explains why it refused.

### What is written down, and when

**The row is written before the dump starts**, for the reason a
deployment row is: nobody is holding the connection, so where the
outcome goes has to exist before there is one. `Take` returns it and the
work runs detached.

**A failure keeps the row.** A backup that did not happen is the thing
somebody most needs to find out about, and a row that disappeared on
failure is a schedule that looks like it is working.

**The name, the engine and the version are stored, not joined.**
`datastore_id` is `ON DELETE SET NULL`, so deleting a database keeps its
backups — which is exactly the moment they matter — and a dump that
could not say which engine and which major version produced it is one
nobody can safely load anywhere. `ErrEngineMismatch` is the refusal that
uses them.

**A dump in flight is `taking`, not `running`.** `running` is already a
word here and means a healthy container; painted the green `StatusBadge`
gives it, a backup that has not finished would read as one that exists.

### The timer

A **time of day** and a timezone, for the reason `update.Scheduler`
takes one: what is being chosen is when the database may be busy and
slow, and "every 24 hours from whenever you turned it on" is not
something anybody can plan around.

**The schedule row existing is what "scheduled" means.** There is no
flag beside it that could say off while a time sat there — the same
shape as `autoscale_max = 0` and `source_tag == ""`.

`last_run_at` is written **before** the dump rather than after it, so a
dump that takes an hour, or a daemon that dies during one, cannot make
the schedule fire again the moment it comes back.

**Retention counts only the successful ones.** A week of failures would
otherwise push the last good dump out of the window, which is the one
moment retention must not be the thing that loses it — and a failed row
is kept regardless, because it is the evidence that a schedule is not
working. `keep = 0` holds every one, which is a decision somebody can
make rather than a gap, and the screen says what it costs.

Pruning runs **inside the dump's own goroutine after it succeeds**, not
in the scheduler: the first version called `Wait` from a goroutine the
same `WaitGroup` was counting, which is a deadlock.

### Restoring

**It replaces what is there and cannot be undone**, so every surface in
front of it asks for the database's own name first. What it does not do
is stop the database: an app writing during a restore produces a state
that is neither the backup nor what was there, and the screen says so
rather than pretending otherwise.

Refused for a dump that has not finished, one that failed, and one from
another engine or major version. A backup whose database has been
deleted can be **downloaded and not restored** — where it should go is a
choice this release does not offer, and picking one on somebody's behalf
would be the wrong database quietly replaced.

### Backing the instance up

This module was built above `datastore` rather than inside it for
exactly this: **the most valuable database on the box is Cubeship's own
Postgres**, which holds every account, project, app, credential and
attachment and is not a datastore at all. A module that could never
reach it would have been the wrong module.

**What the archive is a copy of is Cubeship, not what is on it.** A
logical dump of that database, plus the two things under the data
directory that cannot be worked out again: `letsencrypt/acme.json`,
which is every certificate and its private key — losing it is not fatal
but asking again spends a weekly allowance shared with everyone under
the same registered domain — and the pictures on projects.

**The data is deliberately out.** A datastore's directory and a managed
store's objects are the two largest things on the box by orders of
magnitude, and each already has a backup of its own that can go
somewhere else. Folding them in would make the one artifact that has to
be small enough to take every night the one that is too big to take at
all. The build cache is a cache, the setup token is spent, and
`traefik-dynamic` is written from the rows on every start.

`kind` is what tells the two apart, and it is a column rather than
"`datastore_id IS NULL`" because that already means something else: the
foreign key is `ON DELETE SET NULL`, so a null there is a dump whose
database was deleted. **Retention counts within one kind** — without
that, a nightly copy of the instance pushes somebody's database out of
its own window of seven.

**It is staged on disk, and a datastore's dump is not.** Every other
dump here is a pipe from the engine straight into a multipart upload,
because a database larger than the disk under it is the ordinary case on
a small VPS. A tar entry has to declare its size before its bytes, so
the same trick is unavailable — and unnecessary: what is in here is rows
about projects and apps rather than anybody's data.

**Through the Postgres container, because the daemon has no `pg_dump`.**
Its image is Alpine with one binary in it. The password is read from
inside that container rather than passed in — it is already in its
environment — so the command is a fixed string with nothing
interpolated, which is the rule `firewall.Spec.Args` keeps for the same
reason. An instance pointed at somebody else's database with
`CUBESHIP_DATABASE_URL` has no container to exec into and is **refused
with that sentence** rather than handed an archive with a hole in it.

**There is no restore.** The thing being replaced is the database the
button would be running on. Putting an instance back is a fresh install,
the daemon stopped, the dump loaded with `psql` and the files put back —
an operator's procedure, and the screen says so where the button would
be rather than leaving somebody to find out.

The instance's schedule is a **table of its own with one row**, because
`backup_schedules` is keyed by the datastore it belongs to and this has
none. Everything above the repository reads the two as one list: a
schedule is the same four answers either way, and `Schedule.Kind()`
derives which from the one field that can tell.

### The role, and the surfaces

An **admin's**, all of it. A dump is every row in the database, so
somebody who may take one may read everything it holds, and a restore
replaces all of it — the line `objectstore` already draws around a
bucket's contents.

The database's **Backups tab** is where the dumps are: the schedule,
"back up now", and that database's own history, polling while one is in
flight. It is under that database's **Settings**, and it is the first
tab there — the other two are set once, and whether last night's dump
happened is a question somebody has again every week. The instance's own
is in the same place for the same reason, under `/settings`.

`/backups` in Platform is the other question, and it is **not a list of
backups**. It was one for a release, and a list of every dump on the box
is a log — nobody reads a log to find out whether they are covered, and
that one could not answer the question anyway. Built from the backups,
it cannot contain the row that matters most: **a database nobody has
ever backed up appears in no list of dumps.** On an instance where
nothing was scheduled it showed an empty table and no hint that anything
was wrong.

So it is one row per **database**, from `GET /backups/coverage`, which
is built from the databases instead — every one of them is a row,
whether or not it has ever been dumped. Five states, worst first,
because a report sorted by name makes you read every row to find the one
that needs you:

```
never backed up   nothing exists                        ← worst
failing           the last attempt did not succeed
on this machine   a dump exists and goes with the disk
protected         restorable, and somewhere else
not backed up here   an engine this instance does not dump
```

**`protected` is two facts and needs both**, because either alone is
something somebody acts on and should not: a dump beside the database it
came from goes with the disk, and a schedule that has never produced one
is a plan rather than a backup. **`failing` is not its opposite** — last
week's dump can sit safely in a bucket while every night since has
failed, and that is exactly the case worth saying out loud, so both are
reported and both are rows.

An engine with no dump is a **row rather than an omission**: a database
missing from a coverage report reads as one nobody checked.

Every row links to that database's own Backups tab, which is where you
act — `?tab=backups`, read through `useSearchParams` rather than
`window`, because this page renders on the server first and a `useState`
initializer reading a window that is not there hydrates to "overview"
and keeps it.

The **orphans** — backups whose database has been deleted — are the
second half of the screen and their own table, from `GET
/backups/orphans`. Kept on purpose, since deleting a database is exactly
when its backups matter, and reachable nowhere else. Separate rather
than mixed in because **none of them can be restored**: where to put one
is a decision this release does not make, and a table where some rows
can be restored and some cannot is one somebody reads wrong.

**Every surface names an object store by its name**, never its id; the
service resolves between them (`objectstore.IDForName`, `NameForID`).
The schedule row holds the id, because a row pointing at another row
should not hold a slug somebody may reuse.
