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

### The role, and the surfaces

An **admin's**, all of it. A dump is every row in the database, so
somebody who may take one may read everything it holds, and a restore
replaces all of it — the line `objectstore` already draws around a
bucket's contents.

`/backups` in Platform is every backup on the instance, and it exists
for the one row a database's own tab can never show: the ones whose
database is gone. The database's **Backups tab** is the other half — the
schedule, "back up now", and that database's own history — and it polls
while one is in flight.

**Every surface names an object store by its name**, never its id; the
service resolves between them (`objectstore.IDForName`, `NameForID`).
The schedule row holds the id, because a row pointing at another row
should not hold a slug somebody may reuse.
