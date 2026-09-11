# Monitoring

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

`internal/metrics` records what every container on this instance is
using and answers the series a chart is drawn from. `app`, `datastore`
and `objectstore` all serve it, at their own addresses.

**One module, because it is one question.** An app, a database and the
MinIO behind a managed store are all a container with a CPU and a
resident set, and the chart is the same chart. This package knows about
none of them: a `Subject` is a kind, an id and a container, and the
modules that have those hand them over through `metrics.Source`. The
read endpoints live at `/apps/{ref}/metrics`, `/datastores/{name}/metrics`
and `/objectstores/{name}/metrics`, where each module has already decided
who may look — `metrics.Service` takes no caller and checks no role,
because asking twice is two answers to a question with one.

**A linked object store is refused rather than answered.** It is
somebody else's server, so there is no cgroup here to read and never
will be — and an empty series is not that sentence: it reads as a store
sitting idle. `MetricSubjects` drops it on the same test that drops a
stopped MinIO, since a row with no container id has nothing to sample
either way.

**In Postgres, because it is the only store this instance has.**
Cubeship runs on one VPS with no external services; "add Prometheus" is
not a smaller answer than a table, it is a second thing to install, run,
back up and reach. `metric_samples` has one row per container per
interval, and `kind` is what tells an app's from a database's from a
store's — deliberately not a foreign key, since there is no one table to
point at.

**Sampled every 30 seconds, kept for a day.** There is no downsampling
behind that, so a day is what there is: a week of raw rows per container
is twenty thousand nobody looks at. What a day buys is the question
people actually ask — what happened overnight. `Prune` runs on every
collection pass rather than on a timer of its own, because the pass is
already the thing that knows time has moved.

**The percentage is computed at collection time, not on read.** A CPU
percentage is a difference, and `ContainerStatsOneShot` returns counters
with nothing to subtract from — so the collector holds the previous
reading per container and does the subtraction. Keyed by container id
rather than by subject, because a redeployed app is a new container and
comparing across the swap would produce one impossible reading. The
first sample after a restart reports 0 rather than a guess: an invented
first point is a point somebody reads as a fact.

The alternative was the Engine's own `stream=false`, which computes the
delta for you by sleeping about a second first — a second per container
on every pass, for a percentage averaged over that second rather than
over the interval anybody is charting.

**100% is one core.** 250 means two and a half. Not rescaled to a share
of the machine, because that hides how much work something is doing
behind how large the host is.

**Memory is usage minus reclaimable page cache**, which is what `docker
stats` shows. Without the subtraction every container looks about to run
out. The key is spelled `inactive_file` under cgroup v2 and
`total_inactive_file` under v1, and `dockerx.ContainerStats` handles both.

The collector runs in `cmd/cubeshipd`, not in `server.New`: a server is
a request handler, and a test that builds one must not thereby start
polling Docker every thirty seconds.

Bucketing is in SQL (`date_bin`), against a fixed origin rather than
`now`, so two charts loaded seconds apart line up instead of each having
its own grid. Every window buckets to around `TargetPoints`, so a chart
is the same density whichever is asked for.

**On the dashboard**, `MetricsSection` is one component for every page
that charts a container and `TimeSeries` is the chart, over **Recharts**.

It was a hand-drawn SVG first, and that was the right call for exactly
one chart: a line, a fill and a crosshair is not a dependency's worth of
work. It stops being right at the second chart — a bar, a second series,
a legend, a brush is a rewrite of the geometry each time, and each
brings edge cases nobody here has hit yet. What Recharts asks in return
is a theme, and a theme is one file rather than one component per page.
**Charts go through `TimeSeries` or beside it**, sharing that theme; a
page does not reach for the library directly, for the same reason a page
does not restyle a field.

What did not move is the look — 1px rules, a gradient under the line,
the reading over the top left and the peak over the top right, no
shadows — or the two load-bearing decisions. The grid is at quarters of
the box rather than at the scale's own ticks, so it does not move as the
data does. And the scale comes from the data rather than from the memory
ceiling: drawn against the ceiling, a container using 200 MiB of a 2 GiB
cgroup is a flat line along the bottom — a chart that has given up its
only job to answer a question the caption answers better.

### What is using it

`/instance/containers` is the newest reading of every container on the
instance at once — apps, databases and managed stores — heaviest CPU
first. It is the counterpart to the machine's own charts: those say the
box is at 80%, and this says which of the twenty things on it is why.

**The join is in memory, against the subjects the modules already hand
over.** There is no table to join to — an app, a database and a store
are three of them — so `metrics.Subject` carries a `Name` beside the id
it already carries, and `Service.Usage` matches the readings to it.
A reading whose subject is not in that list is dropped, which is exactly
a container that has gone since it was taken; `UsageWindow` is the other
half, so nothing older than two passes is reported as what something is
using *now*.

The name is the reference, never the bare one: `gateway` is unique
inside one environment and nowhere else, so an instance-wide list
calling something `gateway` names what the reader cannot find.

It is served by `machine` rather than by any module that has containers,
because the question is not about any of them. `server.New` hands the
same three sources to the series service that it hands to the collector.

### The machine under them

`internal/machine` is the box itself: how much of its CPU is busy, how
much of its memory is spoken for, how full the disk everything is kept
on is, and how fast bytes are moving over its own interfaces. Served at
`/instance/metrics`, drawn on the Overview — the screen the dashboard
opens on — and a **member's** to read — what the box is doing is the context for every
"why is this slow" anybody deploying here will have, and it says how
much of the disk is left, never what is on it.

**Its own module and its own table.** `metrics` answers one question
about a container — a CPU and a resident set, through the Engine — and
this is a different set of measurements about something that is not a
container: no cgroup has a disk filling up, and the Engine has no
opinion about the wire. Folding them together would be four columns that
are NULL for every row but one kind's. What is shared is imported rather
than copied: the interval, the retention, the windows and the bucketing.

**100% is the whole machine here**, the opposite of the container
convention two paragraphs up. Both are right where they are — what share
of the host one container is taking, and how much of the host is left —
and each says so on its own chart, because it is the one thing about
these numbers that surprises people.

Three of the four are read straight out of `/proc` whatever the daemon
is, since `/proc/stat` and `/proc/meminfo` are not namespaced. **The
network is the one that needs the mount**, and the reason is in the
install section above. Without it the answer is *nothing, with the
sentence saying what to do* — never zero, and never this container's own
veth: an instance reporting no traffic is one somebody investigates, and
one reporting the wrong traffic is one nobody ever does. The missing
measurement comes back inside a normal 200 in `unavailable`, keyed by
what could not be read, because the three that worked are still worth
having.

Two smaller things that are decisions rather than details:

- **`guest` and `guest_nice` are not added to the CPU total.** The
  kernel already counts a guest tick inside `user` and `nice`, so adding
  them makes the total larger than the time that passed — which reports
  a busy machine as idle, on exactly the kind of box somebody virtualises.
  `iowait` counts as idle for a related reason: a machine waiting on its
  disk is not a machine short of CPU, and reading it as busy is how a
  slow disk gets diagnosed as a small box.
- **Memory used is total minus *available*, not minus free.** Free
  excludes the page cache, which is every file the machine has ever
  read, so the other arithmetic reports every box at 95% and leaves it
  there.

**The first pass of a daemon's life writes nothing.** Every rate here is
a difference and there is nothing yet to take one against; a zero at the
left edge after every restart is a point somebody reads as a fact. A
machine whose numbers cannot be read at all — a Mac running `make dev` —
records nothing rather than a row of zeros, and says so once in the log
instead of every thirty seconds.
