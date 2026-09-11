# More than one machine

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

An instance is a **control plane** and any number of **workers**. The
control plane is the box somebody installed: the database, the
dashboard, the registry, the builder, and every decision. A worker is a
second box running the same image in a mode where it decides nothing.

`internal/node` is the registry of machines and `internal/worker` is the
agent. They are two modules because a worker is **not the control plane
with features turned off** — it is a different program, and building it
as a mode of the other would have meant every module growing a branch
for a case where none of them run. `cmd/cubeshipd` branches on the first
line of `run()` and the two never meet again.

**The worker dials the control plane. Nothing dials a worker.** That is
the decision everything else here follows from. A control plane that
called into its machines would mean every one of them publishing an
authenticated API to the internet, with a certificate and a firewall
hole each, and a box behind NAT could not join at all. Dialling out
costs a reconcile loop and buys a worker whose entire network presence
is one outbound request — the same argument `internal/firewall` makes
about published ports, from the other side. A worker publishes **no
port**, not even a health check: `install.sh` waits for a line in its
log instead.

**A pass is a join and a heartbeat at once.** There is no handshake:
`POST /nodes/agent/reconcile` says what the machine is and is told what
it should be running, every ten seconds, and the first one is what
joining means. The endpoint is `HandleInternal` — machinery between two
daemons, like the registry's webhook — and it is behind its **own**
middleware. A node is not an account: it holds no role, and the
question "what could a machine reach" has one answer, which is what is
registered behind that wrapper.

**Adding a machine mints a credential and shows it once**, like an API
key, and contacts nothing: the row is a place for a box that does not
exist yet. Two steps that do not touch each other — the API mints, and
somebody runs `install.sh --control-plane <url> --token <token>` there.
`cubeship server add` is the two put together at the one moment they
can be: it prints that command with the address and the credential
already in it, because the CLI is signed in to the instance being joined
and the token will never be readable again.
Removing one is **local**: the row and its credential go, the agent is
refused on its next call, and nothing on that machine is touched. A
delete that reached out would be a delete that hangs on a host nobody
can dial.

**A status is derived from `last_seen_at` on every read**, never stored.
A column saying a machine is up is only as honest as whatever was
supposed to update it, and the moment that misses a pass the table is
lying about a box that is gone. Three missed passes is `unreachable`, so
one slow reconcile does not flip a healthy machine to a fault and back.

The control plane is a **row in the table**, seeded by the migration.
A listing of "the other servers" is one that cannot answer where
something runs — and everything that will later be placed on a machine
has to be placeable on this one.

### The network between them

`internal/mesh` is **Docker's own overlay network, and nothing else
about Docker Swarm**. Swarm mode is an orchestrator — a scheduler, a
store, services, secrets — and none of that is used: Cubeship decides
what runs where, and a second thing deciding that would be two answers
to one question. What is used is the wire.

The alternative was WireGuard, keyed and peered here: a keypair and a
subnet per machine, peers down the reconcile loop, routes programmed on
each box. That is a tunnel and no more. **Docker's embedded DNS is
per-daemon**, so names would not resolve across machines and every
address would have to be worked out here and injected — which is a
cluster DNS or an addressing scheme, on top of the tunnel. An overlay
gives connectivity and resolution together, so `cubeship-db-pg` means
the same thing on every machine and none of the modules that address a
container by name had to learn where it is.

**It is a second network beside `cubeship`, not a replacement.**
Converting a bridge in place is not something Docker can do — the
network would have to be removed, which means disconnecting every
container on it — so an instance that added a machine would have to take
everything down to gain a network it is not yet using. A container joins
the mesh the next time it is created, which is the rule its labels and
its environment already follow.

**It comes up when there is a machine to join it**, on the first
`POST /nodes`, for the reason BuildKit starts on the first build: an
instance that never adds a second box never becomes a swarm manager. It
comes up *there* rather than on the first reconcile because that is the
moment somebody is watching — an instance with no public address cannot
build a cluster, and being refused now beats a server that says `ready`
and can reach nothing.

**The network is encrypted**, and that is not a precaution about
internal traffic — it *is* the traffic. Every name arrives at the
control plane and is proxied to a container that may be on another
machine, and TLS ends at the proxy: what crosses the wire between two
boxes is plain HTTP with its Authorization headers and its session
cookies in it, plus every connection an app on one machine makes to a
database on another. Between two VPS that is the provider's network and
quite possibly the open internet.

What it costs is small enough to name so nobody has to guess: IPsec ESP
with AES-GCM, in the kernel, on hardware with AES-NI — every server CPU
of the last fifteen years. The cost is proportional to bytes rather than
to packets, and the VXLAN encapsulation it rides on already costs more
per packet than encrypting the contents does.

**Docker fixes the flag when the network is created and offers no way to
change it.** So an instance whose mesh came up before this asked for one
keeps an unencrypted network, and `GET /nodes/mesh` is what says so —
`cubeship server list` prints it under the table, because nothing about
it is visible any other way and the fix is not something to do behind
somebody's back: removing the overlay takes every container off the
cluster's network until each is created again. `mesh` is reserved as a
machine name for that endpoint, the way `agent` is.

**What it costs is three ports open between the machines**: 2377 to
join, which only a manager listens on; 7946 for the gossip that carries
which container is where; 4789 for the VXLAN the traffic goes over. They
are the host's own, so they are `host` scope and none of this needs the
DOCKER-USER adoption an `apps` rule would. `mesh.Admit` writes them
**scoped to the peers' own addresses** — which the control plane has,
because every agent reports one — so a cluster port is open to the
cluster and not to the internet. Rules are added and never removed: one
admitting a machine that has left is a port open to an address that used
to be here, and one removed while the cluster needs it is a machine that
drops off the network.

On each machine the order is **firewall first, swarm second**. Joining
is an outbound call, which a firewall allows anyway; the gossip and the
traffic that follow are inbound from the peers. Joined first, a machine
joins and then cannot be reached, which reads as a swarm that half
worked. Writing a rule costs a container through `hostexec`, so both
sides do it when the peer set changes rather than on every pass.

**Three kinds of container join it, and the rest do not.** An app, a
database and a managed object store — the three an attachment addresses
by container name, which is exactly the set that has to mean the same
thing from another machine. Postgres, BuildKit and the dashboard are
this machine's own, and Traefik is each machine's own edge; putting them
on a cluster network would be reach nobody asked for.

They join it **beside** the local bridge, not instead of it: everything
on this box already resolves them there, and `ContainerOpts.AlsoNetworks`
is the extra. The connection is made after the container is created and
**before it is started**, so its process has the interface from the first
instruction it runs — a server that binds or registers at startup would
otherwise come up without it. A container that cannot join is removed
rather than started: one on fewer networks than it was asked for would
come up, pass its health check, and be unreachable from half the
cluster.

`node.MeshNetwork` is what each of them asks, and it asks the **Engine**
rather than this module's own state — the network can be removed by
hand, an instance can be upgraded into a cluster that already exists,
and a daemon restart forgets everything but the table. The answer is
cached for half a minute, because it is asked once per container created
and changes once in the life of an instance.

**An app is on the mesh under a name that outlives its deploy.** Its
container carries the deployment's id, so the address one app holds for
another is a `ContainerOpts.Aliases` entry — `cubeship-<project>-<env>-<app>`
— attached to the bridge and the overlay alike. Attached to both or it
is worse than absent: a name that resolves here and not there works
until the app it addresses is placed on another machine. See
[networking.md](networking.md).

**Existing containers are not touched.** One joins the mesh the next
time it is created, which is the rule its labels and its environment
already follow — so adding a machine to a cluster leaves everything
running, and a redeploy is what puts an app on the cluster's network.

**A machine behind NAT cannot be in the mesh.** The data plane is VXLAN
between the nodes themselves, so they have to reach each other directly.
That is the limit this design pays for everything else with, and it is
the one thing the "workers dial out" property does not buy back.

**The firewall at the provider is still the operator's.** Contabo,
Hetzner and DigitalOcean filter in front of the machine, and Cubeship
cannot see that layer — the ufw rules it writes do not reach it.

### Where an app runs

An app runs on the machines in `app_nodes`. **Where its traffic arrives
is not one of them**: every name this instance serves arrives at the
control plane, which routes it to whichever machine runs the app. See
"One front door". `PATCH /apps/{ref}` takes `nodes` for the set and
`scale` for how many copies; `node` is shorthand for a set of one, and
`spread` is the switch below.

**An app can follow the cluster instead of naming machines.**
`apps.spread` is that switch: on, the app runs on every machine there
is, and is re-spread the moment one is added or taken away. It is not a
third way of placing things — `nodes` and `scale` already say where and
how many, and what neither can say is "wherever the cluster goes",
which otherwise means editing every such app every time a server joins.
Naming machines turns it off, because that is choosing by hand, and it
is turned off rather than refused: what somebody just said is what they
want.

The seam is `node.Apps.Rebalance`, declared by `node` and satisfied by
`app` — the same direction `PlacementsFor` runs. `node` is what knows a
machine was added or is about to go, and what "following the cluster"
means is `app`'s to decide. It runs **before** a machine's row is
deleted, and is told which machine that is, so a following app leaves
rather than standing in the way of the delete. An app somebody placed by
hand still refuses it, and that is the difference: where that one should
go is a decision, and making it by deleting a row would make it
invisibly.

**Scaling takes effect now, in both directions, on every machine.** A
worker creates the copy that is missing on its next pass — woken, so
within a second — because reconciling is what its loop is for. The
control plane had no such loop: the same request wrote a row and left it
without a container until somebody happened to redeploy, so one request
meant two different things depending on which machine the app was on.
`Orchestrator.fill` is that half, and it runs the deployment the app
**should already be running** — `DeploymentToRun`, the same question a
worker is answered with — so a new copy comes up on exactly the version
its neighbours are on. Nothing is built and **no deployment row is
written**: scaling is not a deploy, and putting a rollout in the history
for a decision that changed no code would be one nobody made.

Scaling down stops the copies at once, on this machine and on a worker
alike. A container left behind is one nothing on the instance names any
more — invisible on every screen, holding its memory, and answering on
the mesh under a name the proxy no longer points at. It is not the
outage it looks like either: the proxy is built out of the rows a
placement rewrites, so a name loses its old backends the moment they
stop being what should run, and keeping the container alive would keep
nothing serving.

`app_nodes` is **desired and actual in one row**: the row existing means
"run a copy here", and its container columns are what is actually there.
Two tables would have been two answers to "is it up".

**A machine can run several copies**, and the ordinal on the row is what
tells them apart. So the row count *is* the scale: four rows is four
copies, and where they sit is the spread. `Spread` divides a number over
the machines round-robin in their own order — four over three is 2, 1, 1
— and it is deterministic because the answer decides which containers
exist: a spread that moved between two reads would be a machine told to
start a copy and then told to stop it.

**How many is a number for the app, not one per machine.** That is how
scale is thought about — "run four of these" — and a count per machine
would be a third decision to keep in step with the other two by hand.
What it gives up is a different count on machines that are not alike,
three on the big box and one on the small one, which is not
representable. `scale` is the number and `replicas` is the list; two
names because they are two shapes of one fact, and sharing a name is how
a client sends an array where a count was meant.

**What was asked for is stored, and it is not the row count.**
`apps.scale` is the intent and **zero means one per machine**, which is
what every app is until somebody says otherwise. The rows cannot hold
that answer — "however many machines there are" is not a number — and
reading the intent off them is a bug that already happened: taking a
machine away from an app running one copy on each of two left *two*
copies on the survivor, so somebody moved an app off a box and got a
second copy on the other one. A chosen number survives the machines
changing under it, because it is a decision; a count nobody chose does
not, because it was never one.

Scaling up and scaling out are **separate acts**: sending `scale` alone
leaves the machines as they are, and sending `nodes` alone keeps the
intent and re-spreads it. Neither silently does the other.

**Never fewer copies than machines.** A machine an app was placed on and
given nothing to run is a machine somebody put it on for no effect, so
asking for that is asking for fewer machines, and `Spread` says so by
raising the count rather than leaving a machine empty.

**The first copy keeps the plain container name**, and only the second
and later carry a suffix — the same trick `traefik.Labels` uses for the
first router. An app that runs one of itself, which is every app until
somebody asks for more, has exactly the container it always had.

**The split is between deciding and doing**, and it falls where the two
things each machine has are. The control plane resolves the image,
records what it resolved to, and stops; the machine the app is on
creates the container. `internal/app/placement.go` is that seam:
`PlacementsFor` builds one **complete instruction** per app — an image,
a registry login, an environment, labels and networks — because the
machine has no database and no way to ask a second question.

Complete is the load-bearing word. A scoped read carries no domains, so
a placement built straight off one goes out with no Traefik labels at
all: the machine serves none of the names of the apps it runs, and
because `applyEdge` decides whether to start a Traefik by looking for
those labels, it never starts one. `PlacementsFor` fills them in for
that reason, and `TestEveryMachineRunsItAndNoneOfThemRoutesItAlone`
pins it — the failure is silent from every side, which is what made it
worth a test rather than a comment.

**A remote deploy stays `pending` until the machine says what it did.**
`Placed` is where it ends: the row becomes succeeded or failed and the
app points at the container that is now serving it, which is exactly
what a local deploy writes at the same point. Nothing here waits on it.

**What a machine should run is the newest deployment that resolved to an
image and did not fail** — not simply the newest. A deploy the machine
rejected is one it should stop trying, and the row under it is what it
should be running instead. Rollback falls out of asking the question
that way rather than being a path somebody wrote.

**A deploy on one machine with several copies is a rolling one.** Each
is brought up and proved healthy before the one it replaces is stopped,
one at a time, which is the single-copy swap taken in a loop — an app
with one copy takes it once and cannot tell the difference. A copy that
will not come up stops the rest, so the ones already swapped keep the
new version and the ones after it keep the old: a split this instance
reports rather than hides, and better than carrying on into an app that
is entirely the version that does not work.

The agent's own half is two halves in one order: **start what is
missing, then remove what is not wanted.** The reverse takes an app down
and then finds out its replacement will not start. A container whose
replacement is not up is left exactly where it is, which is what makes a
failed deploy a no-op rather than an outage. What it removes is what
carries `cubeship.app` and is not in its answer — a container with no
such label is not this instance's to touch.

A placed container is named for its **deployment id** rather than for
the moment it was created, so a machine told the same thing twice can
answer "am I already running this" from the name.

**And it carries which copy it is**, in `cubeship.ordinal`. That is what
makes a rolling deploy possible on a machine running several copies: an
old container may go once **its own** replacement is up. Without it the
agent can only ask whether some container of that app is running, which
either retires a copy whose replacement never came or leaves every old
copy behind. What the set of wanted containers is keyed by matters for
the same reason: it was keyed by the *app*, which holds one entry
however many copies were sent — so every copy but the last read as
unwanted, was removed, was started again on the next pass, and flapped
for the life of the instance. It could only happen on a real worker
running a scaled-out app, which is exactly the thing nothing here had
ever run.

**A machine is told where it can actually pull from.** An app on this
instance's own registry resolves to the registry container's name on
this box's bridge — deliberately, since pulling the public name here
would hairpin out to the VPS's own address and need a certificate to
already exist. Sent to another machine that name resolves to nothing:
the registry is this machine's own, like Postgres and the builder, and
is not on the mesh. `Orchestrator.publicImage` is the one rewrite, and
it only ever touches this instance's own registry — every other
reference in a deployment already means the same thing everywhere.

**The ordinal goes back untouched in the result**, and it is what says
which copy a report is about. An agent that drops it makes every report
one about a copy nothing asked for, so every remote deploy stays
`pending` for ever, on every app, with nothing anywhere saying why —
which is what happened when the field was added to both ends of the wire
and filled in on only one. A missing ordinal is read as the first copy,
because that is the only one an agent from before this could be running.
`TestTheAgentReportsWhichCopyItRan` is the first test `internal/worker`
ever had, and the bug is why it exists.

**A worker pulls from this instance's own registry as itself.** The
control plane cannot put that credential in a placement — it holds only
the hash — so what travels is the registry's *host*, and the machine
logs in as `cubeship-node` with the credential it already dials home
with. `internal/registry` grants it **pull and nothing else**: a machine
that decides nothing has no reason to hold a credential that could push.

**One refusal, and it is a thing that would otherwise not work in a way
nobody would notice**: an app that **builds** cannot leave the control
plane on an instance with **no domain**, because a build reaches another
machine only through this instance's own registry and the registry
follows the domain. With one it is placed like anything else — the build
still happens here and its result is pushed rather than loaded. See
"Building images". The check is in two places on purpose: in
`checkPlacement`, which is a sentence in front of somebody making the
decision, and in `buildTarget`, which is the one that cannot be skipped.

**A deploy is finished when every machine has it.** `settle` is the
rule: the row stays `pending` until every replica reports that same
deployment running. Marking it succeeded when the first machine has it
would report a rollout that is a third done as finished, and the two
still pulling would look like nothing was happening. One machine
failing fails it **at once** rather than at the end — a deploy that is
going to be reported failed should say so while somebody is watching —
and the machines that did start it keep what they started.

**A deploy is also closed when the set it was waiting on changes.**
`settle` is otherwise reached only from the two moments that report
something — the end of a local deploy, and a machine saying what it did
— which leaves the case where nothing is reported and the answer
changes anyway: a machine the deploy was waiting for is taken off the
app, and what is left is already running it. Nothing was coming to
close that row, and a deploy that has not finished cannot be deleted
either, so it sat there for the life of the instance. `settleOpen` is
the re-ask, and `replace` is where it happens.

**A deploy waiting on a machine that has stopped answering stays
`pending`, and says who it is waiting for.** That is `Stall`, derived on
read from how long the machine has been silent — one that is merely
slow, pulling an image or starting a container, is still calling in
every ten seconds.

**The machine's silence decides, not the deploy's age.** A deploy
started two minutes ago onto a box that died this morning is stalled
now, and waiting another quarter of an hour would not make that truer.
`StuckAfter` is long because the cheap answer is already taken:
`unreachable` is three missed passes, which is the right patience for
pulling a machine out of a load balancer — where being wrong costs one
interval of traffic — and nowhere near enough here, where being wrong
tells somebody a rollout is never happening while their box reboots.

It is deliberately **not** marked failed, and the reason is what
`DeploymentToRun` does: it takes the newest deployment that resolved to
an image and did not *fail*, so a pending one is exactly what a machine
picks up when it comes back, finishing the rollout it missed. Failing it
would send that machine to the deployment below while the machines that
took it stay on this one — an app running two versions with nothing on
any screen saying so, because a replica running *something* reads as
`running`.

So what changes is what is said, not what is run: a screen names the
machine instead of showing a spinner that never ends, and the record
becomes **deletable**, because nothing is writing to it any more.
Clearing it is a decision with a consequence — the machine that returns
will run the version below — and it belongs to a person rather than to
a timer, which is the same place every other irreversible act here
lives.

**Whether its machines agree on a version is a second question**, and
`Split` is it. An app can be degraded and split, or running and split,
so it is reported beside the status rather than as one of its values —
and every replica carries the deployment id it is running, which is what
turns "two versions" into "this box is on #11 and that one on #12".

It is a **fact, not a fault**. Every rollout across several machines
passes through it for as long as the last machine takes to pull. What
makes it worth reporting is the rollout that never finishes: each
replica is running *something*, so without this an app serving two
versions reads as one healthy app.

**An app's status is derived from its replicas**, never stored: `running`
when every one is serving, `down` when none is, and **`degraded`** when
some are and some are not. That last state cannot happen to an app on one
machine, which is why it did not exist before there was more than one.
Reporting it as `running` would hide an outage, and as `down` would
invent one.

**Its log and its charts are per machine.** A log belongs to one
container, so `?server=` names which and the default is the edge —
interleaving three of them would need a clock those machines do not
share. The charts are one series with a reading per replica in each
bucket, so an app's chart is **the average across its replicas** and its
peak is the busiest one's.

### One front door

`internal/app/routing.go` is the load balancer, and it is Traefik's own
round-robin pointed at container names.

**Every name arrives at the control plane.** Its Traefik is the proxy
for the whole instance: one router per name, whose backends are every
copy of that app anywhere in the cluster. A worker runs no proxy at all
— no certificate store, no :80 and :443 held open, nothing to bootstrap.

**The certificate is what decided this.** A machine that routes a name
asks Let's Encrypt for it over TLS-ALPN on its own :443, so a machine
the record does not resolve to fails that challenge every time, for
ever, spending a limit shared with everyone else under that registered
domain. An edge per app was the way round it, and it cost a DNS record
per app — repointed by hand every time an app moved — and a certificate
store on every box.

Traefik is a load balancer. It was already the thing in front of every
container on this machine, and this is the same job over a network that
now exists.

**What it costs, plainly:** all app traffic arrives here, so it stops
when this box does. Before, an app on a worker outlived a dead control
plane — at the price of a record per app, a certificate per machine, and
an app on two machines being served entirely by whichever one its record
happened to name. That was the trade, and it was the wrong way round.

The earlier reasoning against this is worth recording because two thirds
of it was already false when it was written: "the traffic between them
needs a link of its own" — it has one, the mesh, built the day before —
and "that box becomes what the cluster's uptime is", which was already
true per app, in a form that also made DNS a chore.

**A file, not labels.** Traefik's Docker provider sees the one Engine it
is pointed at, so this machine discovers its own containers and none of
the ones on the rest of the cluster. The control plane is the only thing
that knows where every copy is, so it writes them out and its own
Traefik reads them. **No container carries a Traefik router any more**,
here or anywhere: `placementLabels` emits the network and the two labels
that say whose container it is, and nothing else.

**The backends are container names**, which resolve from any machine on
the mesh. That is what makes this a balancer rather than a list of
addresses that goes stale: a copy is reached by what it is called, and
what it is called was chosen by the placement that created it — derived
on the control plane from the app's reference, the deployment's id and
the ordinal, never taken back from the machine's own report.

**The writer is woken, not only ticked.** A deploy swaps a container,
and until the file is rewritten it names the one that has gone — a 502
on every request for that name. A ticker alone would make that up to a
full interval of them on every deploy of every app, so whatever moves
the set of live containers says so: `Service.SetRoutesChanged` carries
the writer's `Wake` down to the orchestrator, and the tick is the
backstop for what nothing thought to announce.

The file is rewritten **only when it changed**, and rendered sorted, for
one reason: Traefik reloads on every write, and a map's iteration order
would make every pass look like a change.

**Nothing to serve is no file at all.** Traefik refuses a document whose
`http` has nothing under it — "http cannot be a standalone element" —
and refuses it as a failure to build the configuration *at all*, which
takes the whole file provider down and `api.yml` with it. An instance
then stops answering at its own name and stops issuing registry tokens,
while every container Traefik discovered by label goes on working: the
same shape of failure the Traefik version pin exists to avoid. The
provider watches the directory, so a file that goes takes its routers
with it and nothing else.

A copy with **no container name written down** is not a backend. An app
running since before that column existed has one, and its next deploy
names one. Skipping it costs that copy its share of the traffic;
guessing would cost the whole name.


### When the proxy stops trusting a copy

Two mechanisms, and the split is what each can see.

**A dead replica costs a retry, and that needs nothing configured.** A
container that has gone refuses the connection, so the router
carries a `retry` middleware with one attempt per backend: the request
goes to the next replica and the visitor sees nothing. Without it that
refusal is what they get — one request in three failing on an app with
three replicas, for as long as it takes the machine that lost the
container to say so, which is that machine's next pass. It is attached
only when there is more than one server, because attempts count the
first try and a retry against one backend is the same dead container
asked twice.

**A replica that is up and broken needs a health check, and that needs
a path.** It answers the connection, so no retry ever fires for it —
nothing but an actual request tells the difference. `apps.health_path`
is that path, `HealthInterval` and `HealthTimeout` are how hard the
proxy looks, and every service in the file carries it.

**No check is the default, and it is the only safe one.** A path is
something only the app's author knows, and a wrong one does not degrade
a name by halves: Traefik marks every replica down at once and the name
answers 503. So a default of `/` would be this instance turning working
apps off — most of them answer 404 there — and the field is opted into
instead.

The timeout is deliberately not tight for the mirror reason. A replica
taking five seconds to answer is in trouble; one taking a second under
load is not, and marking a working container down is the failure that
takes a name off the internet rather than the one that degrades it.

`ValidHealthPath` is strict because the value is interpolated into a
Traefik dynamic document **and** into a container label: a quote, a
newline or a colon in it is configuration somebody else wrote. Same
argument `ValidHost` makes about `Host(`+"`%s`"+`)`, and the same answer —
the grammar is the rule, and `?` and `#` are left out because a health
check with a query string is not something to guess the meaning of.

**Nothing to balance is no file at all.** Traefik refuses a document
whose `http` has nothing under it — "http cannot be a standalone
element" — and refuses it as a failure to build the configuration *at
all*, which takes the whole file provider down and `api.yml` with it. An
instance then stops answering at its own name and stops issuing registry
tokens, while every container Traefik discovered by label goes on
working: the same shape of failure the Traefik version pin exists to
avoid, and it is why an empty document was the wrong idea. The provider
watches the directory, so a file that goes takes its routers with it and
nothing else.

A replica with **no container name written down** is not a backend. An
app that has been running since before `app_nodes` existed has one of
those — the column did not exist — and its next deploy names one.
Skipping it costs that replica its share of the traffic; guessing would
cost the whole name.



A machine with apps on it cannot be removed — `ON DELETE RESTRICT`, and
the error says to move them. Where they should go is a decision, and
making it by deleting a row would make it invisibly.

### Asking a machine something

A worker dials the control plane and nothing dials a worker, so this
instance cannot ask a machine anything — and a log lives on the box its
container is on. `internal/node/commands.go` is the way round it, and it
adds no listener anywhere: **the request is what waits.**

Somebody opens a remote app's log; the request parks on the control
plane; the machine's own poll is woken and carries the question; the
machine posts the answer back and that releases the request. One poll
rather than one interval, and `internal/app` does not branch on which
machine an app is on beyond choosing that door — what comes back is the
same bytes a local log is, demultiplexed out of Docker's frames by the
machine that read them.

**The poll parks because the machine asked it to.** `AgentRequest.Wait`
is the agent's own flag, which is what makes it safe to add: an agent
from before this existed does not send it, is answered at once, and goes
on polling on its interval exactly as it did.

It parks on **commands**, not on whether there is desired state to send
— there always is, so a machine with an app on it would otherwise never
park and never hear anything promptly again. A woken poll asks what it
should be running a second time, because that may be what woke it, and
it asks through `Service.Desired` rather than through `Reconcile`:
calling that again would stamp the machine as having reported an empty
pass and overwrite what it actually said with zeroes.

**It is all in memory.** A command is a request in flight: it means
nothing once whoever asked has gone, and a row for one would be a row to
clean up. A machine may only answer what was sent to *it* — otherwise a
worker's credential would be a way to feed somebody else's screen
whatever it liked — and an answer nobody is waiting for is dropped
without complaint.

Two budgets, because they are nothing alike: `dialTimeout` on the agent
covers a call home and has to be **longer than the control plane parks**,
or every quiet poll is cut short by the client; `workTimeout` covers what
a pass *does*, which includes an image pull and is minutes.

A deploy placed on a machine wakes it the same way, so it starts in the
second it was asked for rather than in the half-minute after.

### Charting a container on another machine

The daemon that can reach a container's cgroup is the one on its
machine, so that machine takes the reading and reports it on its own
poll. What crosses is a **percentage**, not the counters it came from: a
CPU percentage is a difference between two readings, and only the
machine holding the previous one can take it.

Both sides compute it with `metrics.CPUPercent` — one formula, two
callers — so a chart of a container on a worker is drawn on the same
axis as one here: 100 is one core on either. The reading lands in
`metric_samples` through `metrics.Service.Record`, in the same table, at
the same interval, pruned by the same pass.

**The reading is matched to an app by container id**, never by the name
the machine reports. A container that has since been replaced is one
whose reading belongs to nothing, and writing it against the app anyway
would draw the old version's line on the new one's chart.

A machine polls far more often than a chart wants a point, so the agent
takes readings on `metrics.Interval` rather than on every pass — and
takes none at all until it has something to compare against, which is
the rule the collector here follows too.

### What a container may take

`internal/limits` is the ceiling one container runs under: a CPU quota
in cores and a hard memory limit in bytes. Its own small package,
beside `envvar` and `slug`, because it is one idea two modules have —
an app's container and a database's are both a cgroup with a ceiling —
and neither should import the other to say so.

**Zero is no limit, in either half independently**, and it is what
every container this instance has ever run has had. Nothing capped
anything: one app with a leak could take the machine down, the daemon
and the proxy with it, and nothing here stopped it.

**It is per container, not per app.** Three replicas under a one-core
limit may take three cores between them. That is the only arithmetic
that survives the replica count changing, and it is what every
scheduler with both numbers does.

**It is the one part of a container the Engine can change while it
runs.** Everything else — image, binds, ports, environment — is fixed
at create time, which is why a new setting anywhere else means a new
container. So raising an app's memory is a request rather than a
redeploy: `dockerx.SetResources` writes the cgroup and the process
inside never restarts. A database is the same, and there it is the
difference between raising its memory and a database going away for a
few seconds, the way publishing a port makes it.

For that reason a ceiling is **left out of `bootstrap`'s configuration
fingerprint**: a different one is not a reason to replace a container.
Nothing infrastructure runs carries one, and
`TestNoInfrastructureContainerIsCapped` is what fails if somebody gives
one a ceiling `Ensure` would then quietly not apply.

**Removing a limit is the one direction that waits.** The Engine merges
an update and reads a zero in any field as "leave that one alone", so a
container goes back to uncapped by being created again — an app's next
deploy, a database's next start. Every surface says so where the number
is typed.

The floors are the Engine's: 6 MiB of memory, because a cgroup below it
cannot hold the runtime that would be started in it, and a hundredth of
a core, which is the resolution `--cpus` works to — anything smaller
would round to zero and be read as "no limit", the opposite of what
somebody typing a very small number asked for. Both are checked in the
service, where the person who typed one is still watching, rather than
found out by a container that will not start on some machine minutes
later.

`MemorySwap` is pinned to the memory limit rather than left alone,
because Docker's own default is twice it: the same number would mean one
thing on a host with swap and another on a host without.

**On another machine the ceiling travels in the placement**, and is
re-sent on every pass — which is what makes a limit raised here take
effect there in the next ten seconds rather than at the next deploy. The
agent skips the call entirely for an uncapped container, which is most
of them, and could not lift a ceiling that way in any case.

**A linked object store has none, and asking is refused.** It is
somebody else's server, so there is no container here to cap and never
will be — and a ceiling stored on that row would be a number on a screen
for a machine this instance has no say over. Same shape as
`ErrManagedFixed` from the other side: each kind of store refuses the
setting that belongs to the other.

**No MCP tool sets one.** It is the line `internal/app` already draws
around moving an app between machines, one step closer in: a memory
ceiling below what a container is holding is an instant kill by the
kernel, with no deploy, no confirmation and nothing to roll back to. An
agent can read what the ceiling is — it is on every response — and
cannot move it.

There is **no vertical autoscaling**: nothing watches what a container
is using and moves its ceiling. Raising one on its own would be safe and
reversible; lowering one kills the container on the spot, so a rule that
could only ever go up would be a rule that never comes back — and the
decision about when to come down belongs to a person for now. Horizontal
autoscaling is a different question and is below.

### Where an app's traffic arrives

**At this instance, always.** One address, one certificate store, one
record per name — and moving an app between machines touches none of
them. `Response.Address` is the instance's own public address for every
app, because that is where every record points.

`internal/certificates` is simpler for it: there is no name served by a
machine this one cannot see, so no `another_server` to report and no
store to guess about. A name is routed once something is running the
app, wherever that is — the router is a line in the file this machine
writes, and it does not wait for a redeploy the way a container's labels
did.


### Deciding the count

`internal/app/autoscale.go` is this instance changing an app's replica
count on its own. Off on every app until somebody turns it on, and
`autoscale_max = 0` is what off *is* — there is no separate flag that
could disagree with it.

**One signal, and it is CPU.** It is the only one where adding a copy
changes the number: memory does not fall because there are more
replicas — each still holds what it holds — so a memory rule would climb
and never come back down. Requests per second would be the other honest
signal and this instance does not measure them.

The reading is the **average across the app's copies**, which is what
its chart already is: every replica records against the app, on
whichever machine it is on, through the same `metric_samples` table. So
a target of `100` means one core per copy, on the same scale every
container chart here is drawn on — deliberately, so the number somebody
types is the number they were looking at.

The arithmetic is the one every autoscaler uses, in one small function
that can be read: `running * cpu / target`, rounded up because half a
copy does not exist and rounding down leaves every copy above target,
then clamped to the app's floor and ceiling.

**A ceiling is not optional.** Without one a loop of requests is a loop
of replicas until the machine has nothing left, which is a worse outage
than the one autoscaling was turned on to avoid. The largest accepted is
100, and that is a typo limit rather than a resource one: a ceiling of
1000 on a box that runs a handful of containers is somebody who meant
10, and the rule would obediently work towards it.

**Everything that damps it is fixed, and each number is a way a rule
stops settling**: a ratio within 10% of target moves nothing, or every
pass finds the ratio is not exactly 1 and asks for a count one different
from the one it has, for ever. A window shorter than three readings is
waited out, because a decision from one point is a decision from noise.
And after a change it waits — three minutes before another, **ten before
a smaller one**. That asymmetry is the one worth having: an extra copy
costs some memory, and one copy too few costs the app its latency at
exactly the moment load is coming back.

`autoscaled_at` is a column rather than something held in memory. A
daemon restart would otherwise be a free pass to act again immediately,
and a restart is exactly what an upgrade is — which is when load is
already moving between machines. It is not cleared by somebody editing
the rule either: the cooldown belongs to the rule *acting*.

It acts through `Service.scaleTo`, which goes through `replace` like a
person's own request does. That is what makes an automatic change and a
manual one the same event — same spread, same wake, same local pass,
same re-ask of an open deploy — instead of a second path with its own
set of those to keep in step. It takes no caller, because this is the
instance acting and the authorization on scaling lives where a person
reaches it.

The loop runs in `cmd/cubeshipd`, not in `server.New`, for the reason
the collectors do: a server is a request handler, and a test that builds
one must not thereby start changing how many containers exist.

### What is not there yet

- **A different number of copies per machine.** The count is one number
  for the app, spread evenly, so three on a big box and one on a small
  one is not representable.
- **Autoscaling that moves a ceiling.** The replica count is decided
  for you — see "Deciding the count" — and the ceiling is not. Raising
  one on its own is safe and reversible; lowering one kills the
  container on the spot, so a rule that could only go up would be one
  that never comes back.
- **Any signal but CPU.** Requests per second is the other honest one
  for a web app and nothing here measures it, so an app that is busy
  waiting rather than busy computing scales on nothing.
- **Anything in front of the control plane.** Every name arrives there,
  so it is a single point of failure for *ingress* even though an app
  now survives a copy, or a whole machine, going away. What fixes it is
  several A records or something in front of them, and both are
  decisions about failure — and about certificates, since a second
  machine answering a name has to be able to prove it owns it — that
  this does not make.
