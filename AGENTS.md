# Working on Cubeship

Self-hosted PaaS for one VPS. Go, module `cubeship`, no external services
beyond Docker. `README.md` is the operator's intro; this file is for
whoever (or whatever) edits the code.

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

## Layout

The code is organized by domain, not by technical layer. A module owns
everything about one concept — its entity, its persistence, its use
cases, and every surface it is reached through.

```
internal/
  user/         identities and the API keys they authenticate with
  org/          organizations, memberships, and all authorization
  project/      projects and the environments inside them
  app/          apps, deployments, and the deploy orchestrator
  registry/     who may docker push/pull, and the push webhook
  server/       mounts every module on the HTTP mux and the MCP endpoint
  platform/     infrastructure: database, dockerx, traefik, bootstrap,
                config, authkey, regauth, httpx
  envvar/ slug/ small shared vocabulary
cmd/cubeshipd/  the daemon
cmd/cubeship/   the CLI (cobra), one file per noun
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

Dependencies run one way: `user ← org ← project ← app`, with `registry`
and `server` on top. `server` is the only package that knows every module
exists.

## The OpenAPI document

Served at `/openapi.json`, with a Scalar reference at `/docs`. Both are
unauthenticated, because Scalar fetches the document from the browser
with no credentials to offer.

**The document is the product's API, not an inventory of routes.** It
describes what someone integrating against Cubeship would call:
organizations, projects, environments, apps. It leaves out the daemon's
own machinery (`/healthz`, `/openapi.json`, `/docs`, `/mcp`), the
registry's two endpoints (`docker` and the registry container call
those, nobody else), and API-key self-service, which you do once from the
CLI.

That exclusion is declared, never implied. `httpx.Router` has two
registration methods: `Handle` for a documented route, and
`HandleInternal` for one deliberately left out. A documented route with
no operation, an operation with no route, and an internal route that
*is* documented all fail a test — so the trimming cannot decay into
forgetting. `TestDocumentedSurfaceIsTheProductAPI` pins both lists, so
moving an endpoint between them is an edit someone made on purpose.

It is hand-written, module by module, in each `openapi.go` — not
generated from annotations. Every operation needs a unique
`operationId`, a summary, a tag, and its path parameters declared.

Error responses are `text/plain`, because that is what `http.Error`
writes. `openapi.Unauthorized`, `.Forbidden`, `.NotFound` and
`.BadRequest` carry the shared wording — including why 404 and 403 mean
different things.

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
change one, change both. `env` columns are `JSONB`; go through
`envvar.MarshalJSONB` so a nil map becomes `{}` rather than JSON null.

The daemon runs its own `cubeship-postgres` container (see
`bootstrap.PostgresContainerOpts`) unless `CUBESHIP_DATABASE_URL` points
it at an existing server.

## Authorization

One question, one answer: `org.Service.Authorize`. Everything else
reaches it through `Resolve` on its own module's service.

The two refusals mean different things, and the difference is load-bearing:

- **404** — the caller is not a member of the organization at all, or the
  resource doesn't exist. Identical answers on purpose, so a valid API key
  can't enumerate tenants or app names by guessing.
- **403** — the caller IS a member but lacks the role. They already know
  the resource exists; hiding it would only confuse them.

`/mcp` is authenticated by the same bearer API key and **stateless on
purpose** — the server is rebuilt per request so its tools close over that
request's caller, and no session can be reused across users.

Slugs — orgs, projects, environments, apps — go through `slug.Valid`,
because they become path segments of a registry image reference and
Docker rejects anything else.

## App identity

An app is named by a `app.Reference`: `<org>/<project>/<environment>/<app>`,
which is also its registry repository path and the basis of its
container and Traefik router names. A bare name identifies nothing — it
is unique only within its environment.

`ParseReference` accepts three parts as shorthand for `production`, and
validates every part as a slug, so a malformed reference can never reach
a registry path or a router name.

## Instance settings

The domain and the Let's Encrypt contact address are rows in `settings`,
not environment variables: Cubeship is installed with one command,
reached by IP, and configured from there. `config.Load` therefore
requires nothing, and `config.SeedSettings` carries the old environment
variables into the table once, for an install upgrading from the release
where they were mandatory.

Two things follow from a setting rather than being captured at startup,
because an operator changes them without restarting:

- **The registry host.** An app's push path is derived from its
  reference, never stored, so an app created before a domain existed gets
  a correct one the moment there is one.
- **Whether TLS is possible.** `traefik.Labels` takes it, and a container
  keeps the labels it was created with — an app deployed before
  certificates were possible stays on HTTP until it is redeployed.

Writing a setting re-runs `applyInfrastructure` in `cmd/cubeshipd`, which
is what brings the registry up when a domain appears. It works because
`bootstrap.Ensure` replaces a container whose configuration changed.

## Infrastructure containers

`bootstrap.Ensure` fingerprints the `ContainerOpts` it is given into a
`cubeship.config-hash` label. A container whose label still matches is
left alone or started; one whose settings changed is replaced, because
Docker cannot alter an existing container's image, binds, ports or
environment.

That is only safe because everything those containers must keep is in a
host bind mount. Anything you add to them has to keep that true —
persistent state inside a container's writable layer will be silently
destroyed the next time its configuration changes.

Traefik redirects the whole `web` entrypoint to `websecure`, so plain
HTTP reaches every app and the API without per-router labels. It does not
interfere with certificates: ACME uses the TLS-ALPN challenge on :443,
never the HTTP challenge on :80. Changing that would break the redirect.

## Where an app's image comes from

Every app carries a `source`. Today the only one is `registry` — an image
pushed to Cubeship's own registry — and a value the daemon cannot act on
is refused at creation, because accepting one would let someone create an
app that can never deploy.

The seam is `ImageSource`, in `internal/app/source.go`, and the split is
the point:

- **`Check`** is cheap and runs before a deployment row exists, so a
  misconfiguration is a refusal the caller sees rather than a deployment
  that fails minutes later with nobody watching.
- **`Resolve`** produces the image, inside the detached deploy. A source
  that builds will build there — nobody is holding a connection open for
  it, and the deployment row is where the outcome goes.

`Orchestrator.Start` takes a tag, not an image reference: which image a
tag names is the source's answer. `deployments.image_ref` holds what was
asked for until `Resolve` says what actually ran.

## Deploys are detached

`Orchestrator.Start` records a `deployments` row, returns it, and does
the work on a goroutine with a context of its own. Nothing that asks for
a deploy is holding it up: a client that times out, hangs up, or presses
Ctrl-C stops waiting, not deploying. Both entry points — `POST
.../deploy` (202) and the registry webhook — go through it.

That goroutine recovers. An unrecovered panic there would take the daemon
down and every app it proxies with it, which is far worse than one failed
deploy; the panic becomes the deployment's error instead.

How a deploy went lives in its row, since nobody is on the connection to
be told. `WaitFor` polls it, `?wait=true` does the same over HTTP, and
abandoning either does not touch the deploy.

## Deleting

Each level refuses while the one below it is occupied: an app can always
go (its container is stopped first), a project only once it holds no
apps, an organization only once it holds no projects. Nothing cascades
into stopping containers behind your back.

Deleting an app leaves its images in the registry — reclaiming that disk
needs a registry garbage collection pass, which Cubeship does not run.

## Environment variables

Set at three levels, and an app inherits all of them: project, then
environment, then the app's own, each overriding the last. `envvar.Merge`
computes the result a container runs with; `envvar.Resolve` computes the
same thing but labels each value with the level that won it, which is
what the read endpoints return.

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
HTTP. Docker is always faked. `test/integration` needs a Linux Docker
daemon (`--network host` doesn't reach the host on Docker Desktop) and
sits behind `//go:build integration`.
