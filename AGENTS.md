# Working on Cubeship

Self-hosted PaaS for one VPS. Go, module `cubeship`, no external services
beyond Docker. `README.md` is the operator's intro; this file is for
whoever (or whatever) edits the code.

**This file is the part that applies to every change.** How each area
actually works is in `docs/design/`, one file per area — indexed below,
and worth opening before touching the area it covers.

## Before you commit

```sh
make check
```

gofmt, `go vet` (including the build-tagged integration test, which a
plain `go vet ./...` never compiles), and the unit tests under `-race`.
`make help` lists the rest.

Commit messages: imperative subject saying what changes, body only when
the *why* isn't obvious. **Never credit an AI agent** — no
`Co-Authored-By: Claude`, no "generated with" footer, not in commits, PRs
or code comments.

Work happens on `master`, in the repository root. No worktrees.

## Where the rest is written down

Read the file for the area before changing it. Each one carries the
reasoning as well as the rule, including the things that were tried the
other way and were wrong — which is the part that stops a decision being
re-made by accident.

| Opening | Read |
| --- | --- |
| Anything under `web/` | [dashboard.md](docs/design/dashboard.md) — the layers, the navigation, the components, the look |
| Anything under `site/` | [site.md](docs/design/site.md) — cubeship.dev: the landing page, the docs, and where `install.sh` comes from |
| `internal/node`, `internal/mesh`, `internal/worker` | [cluster.md](docs/design/cluster.md) — placement, replicas, the agent, the network between machines, the one front door, autoscaling, limits |
| `internal/app` | [deploys.md](docs/design/deploys.md) — where an image comes from, the two builders, the GitHub App, who may build, what deleting takes |
| `internal/datastore` | [datastores.md](docs/design/datastores.md) — the engines, attaching, exposing, what is fixed after creation |
| `internal/objectstore` | [object-storage.md](docs/design/object-storage.md) — managed MinIO, linked S3, what a folder is |
| `internal/backup` | [backups.md](docs/design/backups.md) — dumps, the schedule, restoring |
| `internal/certificates`, `internal/firewall`, app domains | [networking.md](docs/design/networking.md) — where an app answers, TLS, and the host's ufw |
| `internal/metrics`, `internal/machine` | [monitoring.md](docs/design/monitoring.md) — what is sampled, and what 100% means |
| `internal/credential`, `internal/extregistry` | [credentials.md](docs/design/credentials.md) — one secret, named by everything that needs it |
| `internal/user`, `internal/setup` | [authentication.md](docs/design/authentication.md) — API keys, sessions, the setup token |
| `install.sh`, `internal/settings` | [installing.md](docs/design/installing.md) — the front door, the data directory, the instance's own settings |
| `internal/release`, `internal/update` | [releasing.md](docs/design/releasing.md) — tags, notes, and replacing the daemon that is running |
| `internal/platform/bootstrap` | [infrastructure.md](docs/design/infrastructure.md) — the config hash, and why state must be a bind mount |
| Adding or moving a route | [the-api-document.md](docs/design/the-api-document.md) — `Handle` vs `HandleInternal` |

## Layout

The code is organized by domain, not by technical layer. A module owns
everything about one concept — its entity, its persistence, its use
cases, and every surface it is reached through.

```
internal/
  user/         identities, the API keys they authenticate with, and the
                one authorization question on the instance
  project/      projects and the environments inside them
  app/          apps, deployments, and the deploy orchestrator
  datastore/    the databases the instance runs, and which apps are
                wired to which
  objectstore/  the buckets this instance can reach — the MinIO it runs
                and the S3 endpoints it holds keys for — and the files
                in them
  metrics/      what every container is using, sampled on a timer — the
                series apps, databases and managed stores are charted
                from
  machine/      what the box under all of them is doing: its CPU, its
                memory, the disk everything is kept on, and the bytes
                over its own interfaces
  node/         the machines this instance is made of — the control
                plane and the workers that dial it, and what each of
                them serves
  mesh/         the private network those machines share, and the
                firewall rules that let them reach each other
  worker/       the daemon running as somebody else's machine: the loop
                that calls home and does what it is told. It runs no
                proxy — every name arrives at the control plane
  registry/     who may docker push/pull, and the push webhook
  credential/   the secrets this instance holds — one secret, stored
                once, named by everything that needs it
  extregistry/  which registry Cubeship does not run, and which
                credential logs in to it
  dns/          which provider writes this instance's records, and the
                credential it writes them with
  github/       the GitHub App: private clones, and deploy on push
  setup/        the first-run flow that claims an instance
  settings/     the instance's domain and contact address
  certificates/ what TLS certificates the instance holds, read out of
                Traefik's own store
  firewall/     the host's ufw, and the one thing it does not cover on a
                machine running Docker
  web/          proxies page requests to the dashboard's container
  server/       mounts every module on the HTTP mux and the MCP endpoint
  platform/     infrastructure: database, dockerx, traefik, bootstrap,
                buildkit, config, authkey, regauth, hostexec, httpx
  envvar/ slug/ small shared vocabulary
cmd/cubeshipd/  the daemon
cmd/cubeship/   the CLI (cobra), one file per noun — the noun is what the
                API's own tag calls it, so `/nodes` is `cubeship server`
web/            the dashboard: Next.js standalone, its own image and container
internal/apiclient, internal/clicreds — what the CLI talks to the daemon with
```

Every domain module has the same shape:

| File | Holds |
| --- | --- |
| `<name>.go` | the entity, its constants and its domain errors |
| `repository.go` | every SQL statement for its tables |
| `service.go` | the use cases — the only place business rules live |
| `http.go` | handlers, routes, and the domain-error → status mapping |
| `mcp.go` | the MCP tools |
| `openapi.go` | the OpenAPI operations for the routes in `http.go` |

**`http.go` and `mcp.go` are adapters and nothing else.** They parse
input, call one service method, and render the result. A rule that lives
in a handler is a rule the MCP surface doesn't have — that is exactly how
the two drifted apart before this layout.

Dependencies run one way: `metrics ← user ← project ← app ← datastore`,
with `registry` and `server` on top. `server` is the only package that knows
every module exists.

Two things travel back down, and both do it as an interface the lower
module declares and `server` satisfies at wiring time:
`project.AppTeardown` (deleting a project stops the containers inside
it) and `app.DatastoreVars` (what an attached database contributes to a
container's environment). `metrics.Source` runs the same way in
reverse: `metrics` knows nothing about apps or datastores, and they
hand it the containers worth sampling.

## The API lives under /api, and the root is the dashboard

`httpx.APIPrefix` is applied in one place — `Router.Handle` and
`Router.HandleInternal` — so a module still registers `GET /orgs` and
still reads that way.

The split exists because the two collided head-on: `GET /setup` is the
API's "does this instance need setting up", and it is also the page that
answers it. A dashboard that cannot name its pages after the resources
they show is broken by construction.

`Router.HandleRoot` is the third method, for what is not the API and does
not move: `/healthz`, `/openapi.json`, `/docs`, `/mcp`, the registry
container's `/v2/token` and `/hooks/registry`, and `GET /` itself. Those
addresses are typed by a person or written into another program's
configuration.

Recorded patterns stay unprefixed, so the OpenAPI document keeps
describing `/orgs` and says where `/orgs` is by ending every server URL
in the prefix. `servertest`'s `Do` prefixes for you; `DoRoot` does not.

## Database

Postgres through `pgx/v5/stdlib` over `database/sql`. Placeholders are
`$1`, `$2`, …, and there is **no `LastInsertId`** — an insert that needs
the new row returns it with `INSERT ... RETURNING <columns>`.

Schema changes are [goose](https://github.com/pressly/goose) migrations
in [`internal/platform/database/migrations`](internal/platform/database/migrations),
embedded into the binary. Add a numbered file with `-- +goose Up` and
`-- +goose Down`; never edit one that has shipped. Postgres has
transactional DDL, so each applies atomically, and they run on every
daemon start.

A `Repository` is a thin value over a `database.Queryer`, so the same code
runs on the pool or inside a transaction:

```go
db.WithTx(ctx, func(tx database.Queryer) error {
    users := user.NewRepository(tx)
    orgs  := org.NewRepository(tx)
    ...
})
```

Each table has a `columns` constant its scan function reads in order —
change one, change both. **And find the queries that spell the list
themselves**, because that is where this actually goes wrong: a join
needs the names qualified, so somebody writes them out, and a column
added later leaves that one query selecting one fewer than the scan
reads. It has bitten twice — `datastore.AttachedTo`, which turned every
read of an app's environment into "expected 16 destination arguments in
Scan, not 14"; and the two queries that resolve an API key and a session
cookie to a person, which made **every request on the instance answer
401**. Neither failure looks anything like its cause. `qualify(alias,
list)` is the answer both modules now use: one list, aliased where a
join needs it. `env` columns are `JSONB`; go through
`envvar.MarshalJSONB` so a nil map becomes `{}` rather than JSON null.

The daemon runs its own `cubeship-postgres` container (see
`bootstrap.PostgresContainerOpts`) unless `CUBESHIP_DATABASE_URL` points
it at an existing server.

## Authorization

**There are no organizations.** Cubeship runs one instance on one VPS,
and a tenant boundary inside it was a level everybody had to name and
nobody could use: one organization existed, every screen asked which, and
every app's registry path carried a component that was always the same
word.

What the organization actually held was a role, so the role is a column
on `users` and `user.Require(caller, minRole)` is the whole question.
`RoleAdmin` and `RoleMember` keep their meanings exactly — a member
deploys published images, an admin also builds source on this host (see
`app.RoleToDeploy`) and configures the instance.

The two refusals are still distinct, and still mean different things:

- **401** — nobody is signed in.
- **403** — somebody is, and lacks the role. Said plainly: they can see
  the instance's projects listed, so hiding one would only confuse them.

The 404-instead-of-403 rule is gone with the tenants it protected. It
existed so a valid API key could not enumerate *other people's*
organizations; with one namespace there is nothing to enumerate that the
caller cannot already list.

`/mcp` is authenticated by the same bearer API key and **stateless on
purpose** — the server is rebuilt per request so its tools close over that
request's caller, and no session can be reused across users.

Slugs — projects, environments, apps — go through `slug.Valid`, because
they become path segments of a registry image reference and Docker
rejects anything else.

## App identity

An app is named by a `app.Reference`: `<project>/<environment>/<app>`,
which is also its registry repository path and the basis of its
container and Traefik router names. A bare name identifies nothing — it
is unique only within its environment.

`ParseReference` accepts two parts as shorthand for `production`, and
validates every part as a slug, so a malformed reference can never reach
a registry path or a router name.

## Environment variables

Set at three levels, and an app inherits all of them: project, then
environment, then the app's own, each overriding the last. `envvar.Merge`
computes the result a container runs with; `envvar.Resolve` computes the
same thing but labels each value with the level that won it, which is
what the read endpoints return.

There is a fourth layer nobody types: an attached **datastore**'s
connection variables, between the environment's and the app's own. More
specific than the environment it is in, and still beaten by an app's own
variable — which is how you point an app somewhere else without
detaching anything. `envvar.SourceDatastore` is what labels it, so the
env screen can answer "where did `DATABASE_URL` come from".

**PATCH merges, PUT replaces.** The merge is one SQL statement
(`database.MergeJSONBMap`), not a read-modify-write, so two callers
setting different keys cannot lose each other's. Reach for PUT only when
"delete everything not listed" is genuinely what you mean — the CLI hides
it behind `env replace --yes`, and the MCP tools do not expose it at all.

## Tests

Unit tests need a real Postgres; there is no in-memory mode. `make test`
starts one (`make db-up`, a container on port 5433) and
[`dbtest.New(t)`](internal/platform/database/dbtest) gives each test its
own schema inside it, dropped on cleanup — so tests stay isolated and can
run in parallel against one server.

A test with no database reachable **fails**, deliberately: skipping would
let `make check` report success for tests that never ran.

For anything above the repository,
[`servertest.New(t)`](internal/server/servertest) builds a fully wired
server and drives it through its real router. Use it from an external test
package (`package app_test`) — that is what keeps it from being an import
cycle.

The suite is deliberately not exhaustive. It covers the things that are
expensive to get wrong: the authorization matrix, deploy ordering and
rollback, transaction rollback, registry scope grants, and MCP parity with
HTTP. Docker is always faked.

**Nothing local touches infrastructure.** `make check` needs no Docker,
no Postgres and no network. Standing that up to find out whether a
function returns the right string is time taken out of the edit-run loop,
and CI is where nobody is waiting.

Two mechanisms draw the line, and they are different on purpose:

- **A build tag, for anything that boots a container.** Those tests live
  in `test/integration` and are not in `./...` at all — a laptop does not
  even compile them. One of them is there for a reason worth copying:
  `TestTraefikAcceptsABalancedRoute` hands the real proxy the routes
  file this instance writes and reads its log, because rendering a
  document and looking at the string proves we wrote what we meant and
  nothing about whether Traefik accepts it. Those are different
  questions, and the difference cost an outage — a document with `http`
  and nothing under it renders fine, parses as YAML fine, and takes the
  whole file provider down. `internal/platform/buildkit` keeps only what needs
  nothing: the frontend version pinned against the library, the two
  refusals that never reach a builder, and a clone from a repository on
  disk.
- **`-short`, for the DB-backed tests.** Those are spread through every
  module and cannot move, so `dbtest.RequireDatabase` skips on it.
  Without `-short` a missing database is still a **failure, never a
  skip** — CI runs that way, and a suite that quietly reported success
  for tests that never ran would be worse than no suite.

| Where | What |
| --- | --- |
| `make check` | fmt, vet, shell syntax, `go test -short -race` — nothing to start |
| `make test-db` | the same tests with the Postgres they want |
| `.github/workflows/ci.yml` | all of it, plus the two a Mac cannot run |

The two a Mac cannot run are `test/integration`, which needs a Linux
Docker daemon (`--network host` doesn't reach the host on Docker Desktop)
and sits behind `//go:build integration`, and `make test-install` /
`make test-uninstall`, which run the scripts on a real Debian.
