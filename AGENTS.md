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
  extregistry/  logins for registries Cubeship does not run
  github/       the GitHub App: private clones, and deploy on push
  setup/        the first-run flow that claims an instance
  settings/     the instance's domain and contact address
  web/          serves the built dashboard out of the binary
  server/       mounts every module on the HTTP mux and the MCP endpoint
  platform/     infrastructure: database, dockerx, traefik, bootstrap,
                buildkit, config, authkey, regauth, httpx
  envvar/ slug/ small shared vocabulary
cmd/cubeshipd/  the daemon
cmd/cubeship/   the CLI (cobra), one file per noun
web/            the dashboard: Next.js, static export, built by `make web`
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

## The dashboard

`web/` is a Next.js app with `output: "export"`, built by `make web`,
copied into `internal/web/dist` and compiled into the daemon. An install
is still one binary.

**Static export means no `[dynamic]` segments.** There is no server to
fall back on, so every route is a real static route and whatever
identifies a resource travels in the query string —
`/apps?ref=org/project/env/name`. The reference is four path components
anyway, and carrying it as one value keeps it whole.

The build output is not in the repository; `internal/web/dist/.gitkeep`
is what keeps the embed legal without it. A `go build` with no `make web`
produces a daemon that serves the API and tells you the dashboard is
missing, rather than a blank 404 at the address the installer told you to
open.

`make web-dev` runs it on :3001 against a daemon on :3000, proxying
`/api` — the proxy exists only in development, because in a build the
daemon serves both.

Package manager is **pnpm** (`pnpm-lock.yaml` is the lockfile `make web`
installs from), and linting and formatting are both **Biome** —
`pnpm lint` checks, `pnpm format` writes. There is no ESLint and no
Prettier.

### How the dashboard is navigated

Four levels, and only the first is in the sidebar:

```
organization   the switcher at the top of the sidebar
  project      /                       the grid of cards you land on
    environment  /projects?ref=org/project&env=…   tabs inside a project
      app        /apps?ref=org/project/env/app
```

There is **no flat list of apps**. An app only means something inside an
environment — `gateway` is unique in `acme/api/production` and nowhere
else — so a page that listed every app on the instance was listing
things whose names do not identify them. A project opens on
`production`, the environment it always has and cannot lose.

The project and the app both travel as a `ref` in the query string
rather than a path segment, because a static export has no dynamic
segments. `ref` carries the organization too, so a link works for
someone whose sidebar is pointing elsewhere — opening it moves the
whole dashboard to that organization rather than showing one page out
of frame.

A project has a **settings screen** (`/projects/settings?ref=org/project`)
rather than a delete button in its header: renaming it, describing it and
destroying it are not the same kind of act, and the last one belongs at
the bottom of a page you went to on purpose. `ConfirmDialog` guards every
irreversible action by making you type the thing's own name — a second
button is no obstacle to a misclick, and the dangerous case is precisely
the one the daemon would happily carry out.

A project's **slug is not editable**, and the screen says why: it is a
path component of every registry reference under the project, so
changing it would break every push already configured against it.

### The selected organization

There is no organizations *page*. An organization is the frame every
other screen sits inside, so it is a switcher at the top of the sidebar
— pick one, create one, delete one — and `useOrg()` is how a page asks
which one. Apps, projects and registry logins all read it; the projects
page, the registries page and the new-app form no longer ask a second
time.

It is deliberately **not in the URL**: a static export has no dynamic
segments, and threading it through every query string would mean every
link in the dashboard has to carry it. It is remembered in
`localStorage`, and a remembered slug that no longer exists falls back
to the first organization the daemon returns rather than leaving the
dashboard pointing at nothing.

### The components

`src/components/ui/` is [shadcn/ui](https://ui.shadcn.com) over Base UI,
generated by `shadcn add` and themed only through the CSS variables in
`globals.css` — nothing else in there is worth hand-editing, and Biome's
linter is turned off for the directory for that reason.

`src/components/` is the layer above it, in the vocabulary of this
product rather than of a component library: `Shell`, `PageHeader`,
`StatusBadge`, `TextField`, `ActionButton`, `ErrorAlert`, `Notice`,
`ValueCard`, `AuthLayout`. A page composes those and reaches for
`ui/` directly for the rest. **A page should not restyle a primitive**
— if two pages need the same thing to look the same, it belongs in
`src/components/`.

### The look

Cyberpunk console: near-black surfaces with a blue cast, **every corner
square** (`--radius` is `0px`, and the whole `--radius-*` scale is zeroed
so the shadcn primitives square themselves rather than being overridden
one `className` at a time), cyan as the interface accent and magenta as
the brand's second light. Depth comes from 1px lines and glow, never
from shadow. The only round things left are status dots, which read as
indicator lamps.

Cyan and magenta are both outside the range where green, amber and red
already mean *state* — running, deploying, failed — so an accent never
competes with a status on screen. `StatusBadge` is the only place a
state is turned into a colour.

Type is **Chakra Petch** for the interface and **JetBrains Mono** for
anything you would type or compare — references, images, hosts,
commands. Both are vendored as woff2 under `web/src/fonts` and loaded
with `next/font/local`: `make web` already needs the network for
`pnpm install`, and a second place a build can fail is one too many.

Buttons, badges, field labels and table headers are uppercase with wide
tracking. **That is applied in `globals.css` through the primitives'
`data-slot` attributes**, not by editing `ui/` — which is what lets a
re-run of `shadcn add` overwrite those files without taking the house
style with it. The `hud-frame`, `bg-grid`, `bg-scanlines`, `text-glow`
and `neon-edge` utilities live there too.

The dashboard is dark and only dark: `<html>` carries `dark` rather than
following the system, because the shadcn primitives carry `dark:` rules
and a visitor whose OS is light would otherwise get half of them.

## Installing

`install.sh` is the product's front door: it installs Docker if needed,
pulls the image and runs it. **The release is the image** — nothing else
has to be hosted anywhere, and an upgrade is a pull.

**The daemon is a container**, a sibling of Postgres, the registry,
Traefik, BuildKit and every app on the `cubeship` network. Each finds the
others by container name.

**`config.InContainer` is what decides every address**, and it is set in
the image rather than by whoever runs it. A daemon on the host is still
supported — that is what `make dev` runs — and reaches the same things
over loopback, with containers reaching back through
`host.docker.internal`. `bootstrap.PostgresDSN`, `LocalRegistryAddress`
and `DaemonAddress` are the three places that branch, and
`TestAddressesFollowWhereTheDaemonRuns` pins both modes: getting this
wrong is not a compile error and not a failure anywhere else, it is a
daemon that starts, looks healthy and cannot reach its own database.

**The data directory must be mounted at the same path inside and out.**
The daemon hands paths to the Engine when it creates its siblings, and
the Engine resolves them on the *host*. A different path inside would
make every one of those binds point at a directory that does not exist.
This is the one thing about running the daemon as a container that is
easy to get wrong and silent when wrong.

Traefik is no longer on the host's network namespace. It took it for one
reason — reaching a daemon at `127.0.0.1` — and it costs more than it
buys, not least that host networking does not work at all on Docker
Desktop, where the Engine runs in a VM.

`make test-install` runs the installer end to end in a Debian container
with Docker replaced by a recording stub. It sources the script minus its
last line — `main "$@"` — so the script itself carries no hook for the
test.

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

## Authentication

Two credentials reach the same middleware. An **API key** is what a CLI
or an MCP client carries in `Authorization: Bearer`; a **session cookie**
is what a browser carries. The header is tried first, so a request
sending both meant the key.

Sessions are rows, not signed cookies, because they have to be revocable:
logging out ends one, and changing a password ends every other one the
account holds. Only the token's hash is stored, like an API key's.

**Two hashes, and the difference is not cosmetic.** API keys and session
tokens are 32 bytes of randomness, so `authkey.Hash` (SHA-256) is right —
guessing them is hopeless whatever the hash costs. Passwords are chosen
by people, so they go through Argon2id with the parameters recorded
alongside each hash. Never hash a password with `authkey.Hash`.

The session cookie's `Secure` flag follows the request rather than being
hard-coded: a fresh install is reached at `http://<ip>:3000`, and a
Secure cookie there is simply never sent back — the sign-in would appear
to work and nothing would stay signed in. `SameSite=Lax` is what stands
in for CSRF tokens.

An account can exist with no password. One an organization admin creates
gets an API key immediately and a password only when it sets one, which
is why every sign-in failure — unknown username, wrong password, no
password at all — is the same answer, and why an unknown username still
pays for a hash verification.

## Claiming an instance

`internal/setup` is the first-run flow, and it exists because the daemon
now starts with no account at all — bootstrap creating a super-admin from
the environment is gone. `POST /setup` creates the account, an
organization and a project, signs the caller in, and closes setup
permanently: `Needed` is "are there zero users", so the *first* account is
the only one setup ever makes.

That check and the insert are one transaction behind
`pg_advisory_xact_lock`, because two people opening the page at once must
not both succeed and the username's unique index would not stop them —
they may well pick different names. The loser gets `ErrAlreadySetUp`
(409).

Everything the account needs is created in that same transaction. A user
row with no organization would be unrecoverable: setup refuses to run
again, and the account it made has nowhere to work.

The account gets a **password and no API key** — its way in is the
session setup starts. A key nobody is ever shown would be a live
credential lying around for nothing; keys are self-service.

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

Every app carries a `source`, and a value the daemon cannot act on is
refused at creation — accepting one would let someone create an app that
can never deploy.

- **`registry`** — pushed to Cubeship's own registry. The push is what
  deploys it. Needs a domain before there is anywhere to push.
- **`external`** — an image in a registry Cubeship does not run. Nothing
  notifies Cubeship when one of those is pushed to, so **there is no
  autodeploy**: a deploy is something you ask for. It needs nothing
  configured, which makes it the one thing an instance can run the minute
  it is installed.
- **`dockerfile`** — built here, from a Dockerfile in a Git repository.
  BuildKit does the clone, so nothing needs git on the host.
- **`railpack`** — built here from a Git repository with **no Dockerfile
  at all**: Railpack reads the code and works the build out.

Both builds autodeploy once the organization has connected the GitHub
account the repository is on. Until then, and for a repository anywhere
else, a deploy is something you ask for.

A building app stores `source_repo`, `source_ref` and
`source_dockerfile`. The repository and ref are not Dockerfile-specific —
anything that builds from a repository needs them — so they are not named
for it. A deploy's tag argument overrides the stored ref, which is how
"deploy this branch" works.

The repository must be `https://`, `http://` or `git://`. That rule is
about what the builder can fetch **unaided**, not about what is safe: ssh
needs a key this instance does not have, and a clone failing on a host
key deep inside a build explains nothing. Only https authenticates what
comes back, and a build runs whatever comes back.

An external app stores `source_image`, the reference minus the tag,
because it has nothing to derive one from. The tag is the deploy's
argument, so an image given with one is refused: an app pinned to a tag
could never be told to run another. A registry app naming an image is
refused too, rather than silently ignored.

A push under an external app's name does not deploy it — the webhook
checks the source. Our registry will accept the push, since the
repository path exists either way, but running an image because something
unrelated landed under that name would deploy a version nobody asked for.

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

## Building images

`internal/platform/buildkit` turns a directory of source into an image,
through a `cubeship-buildkit` container.

**The result is loaded into the Docker Engine, not pushed anywhere.** On
one VPS the image never has to leave the box, and a registry round-trip
would need credentials, a reachable host and a certificate — three things
that can all be missing on a fresh install. `dockerx.LoadImage` imports
the tarball BuildKit writes, streamed through a pipe so a whole image is
never held in memory.

The build context is streamed from the *daemon's* filesystem over the
client session, so the builder container needs no access to it.

Two things about that container:

- **It is privileged**, the only one Cubeship runs that way, because
  building an image means running one.
- **It starts on demand**, not at boot. An instance that only runs images
  it is given never builds anything, and a privileged container idling on
  it is cost with no return. `bootstrap.EnsureBuildKit` is what the first
  build calls; `Ensure` makes it idempotent after that.

The daemon reaches buildkitd over a **unix socket** in a host bind mount,
not a port: it is a build service running as root with no authentication
of its own, so filesystem permissions are the guard and there is no port
for a misconfigured firewall to expose. `Builder` takes an address, so
its tests use TCP instead — Docker Desktop's file sharing refuses to
create a socket in a bind mount, and a socket-only test on a Mac would
prove nothing but the Mac's limits. Everything above the dial is the same
either way.

`Build` calls `ListWorkers` before solving. `client.New` does not dial, so
without it an unreachable builder arrives as a solve failure buried in
gRPC wording rather than as `ErrUnavailable`.

## The two ways of building

They differ in **where the recipe comes from**, and that difference
decides everything else.

A Dockerfile is *in* the repository, so BuildKit clones for itself and
nothing touches the daemon's disk. Railpack has to **read** the
repository to work out how to build it, and that reading happens in the
daemon — so it clones first (go-git, no git on the host), plans, and
hands BuildKit the result. Both then go through one `solve`.

Railpack is used as a **library**, not a binary: `core.GenerateBuildPlan`
produces the plan, and the build itself runs through BuildKit's
`gateway.v0` frontend pointed at `ghcr.io/railwayapp/railpack-frontend`.
No extra artifact to ship, and nothing to keep in step by hand except the
version — which is exactly what
`TestTheFrontendMatchesTheRailpackWeBuildPlansWith` pins. A plan is a
versioned document; a frontend reading one it does not understand fails
for no reason anybody can see.

`GenerateBuildPlan` reports a repository it cannot plan through
`Success: false`, not through `err` — err is for transient failures. Its
logs are what tell someone their repository is missing a start command,
so they become the error rather than being dropped for "planning
failed".

**The app's environment goes into the plan**, not only into the
container. Railpack reads it for the versions and commands a project pins
(`RAILPACK_NODE_VERSION`, a build command), so two apps on one repository
with different environments are two different builds.

Mount caches are keyed per app (`cache-key`), because two apps sharing
one would fight over it.

**Nothing prunes the build cache.** It lives in the data directory and
grows with every build — the same shape as an app's images outliving the
app, which also needs a garbage collection pass Cubeship does not run.
`docker exec cubeship-buildkit buildctl prune` is the manual answer for
now.

## Acting as a GitHub App

One App per instance, registered by whoever runs the VPS; its
credentials are settings. Organizations install that App on their own
GitHub accounts, and **an installation belongs to a Cubeship
organization** — that is what stops one tenant deploying another's
private code by naming its URL.

**The installation, not the payload, decides whose push this is.** The
signature already stops a forgery, but resolving the organization from
the installation is what makes tenancy true rather than merely unlikely:
two tenants can legitimately build the same public repository.

The App's private key and webhook secret are **write-only**. `settings`
reports `github_connected`, never the values;
`TestTheAppCredentialsAreNeverReturned` pins that.

**A delivery with no secret configured is refused, not trusted.** An
endpoint that starts deploys on an unauthenticated POST is a way to make
this instance build anything. Everything else about a delivery answers
200 — GitHub retries what it could not deliver, and a payload this daemon
cannot act on is not something a retry fixes.

An app with no `source_ref` deploys on a push to any branch; naming a ref
is how you opt out. A tag is never a branch.

## Cloning a private repository

`TokenForRepository` mints an installation token, cached until shortly
before it expires — GitHub's last an hour and a clone takes seconds of
that, so minting one per clone would be a round trip against a rate limit
for nothing.

**The token never goes in the URL.** A URL appears in BuildKit's own
progress output, and that output goes into a deployment row and then into
a browser. It is handed over as:

- BuildKit's `GIT_AUTH_TOKEN` session secret, for a Dockerfile build —
  BuildKit's own name for Basic auth with the user `x-access-token`,
  which is exactly what a GitHub App token is used as.
- go-git's `BasicAuth`, for the clone a Railpack build does here.

The JWT is hand-rolled: an RS256 assertion is a header, a payload and a
signature over their base64, and a dependency for that is one more thing
to keep current for no gain. `iat` is backdated a minute, because GitHub
refuses an assertion issued in its future and a VPS clock a few seconds
fast is enough.

No installation found is **not** an error. A public repository needs no
token, and letting GitHub refuse a private one beats refusing a clone
that would have worked.

## What a build's output does

A build is the one part of a deploy long enough that watching it is the
point, so `deployments.logs` is written **while it runs**, on a timer
matched to how fast a dashboard polls — not once at the end.

`deploymentLog` buffers and flushes rather than writing per line, because
BuildKit emits output in small pieces and an UPDATE per piece would make
a noisy build heavier on the database than on the builder. It is capped
at `MaxDeploymentLogBytes` and **keeps the tail**: the reason a build
failed is at the end of what it printed. Truncation says so rather than
leaving a reader thinking they have the whole build.

`Close` writes whatever is left, and the deploy path defers it, because
that last flush is the one carrying the explanation of a failure.

`Image.Local` is what stops the orchestrator pulling something it just
built — a registry that has never heard of that image would be the only
place to look.

## Who may build

`Source.Builds()` is the question authorization asks, and `RoleToDeploy`
is the answer: running an image someone already published is a
**member's** job, turning source into an image is an **admin's**. A build
executes whatever the source contains, on this host, with the builder's
privileges — a different kind of act from running a published artifact.

It binds both creating an app and deploying one. A member who could
create an app they can never deploy would be an odd thing to allow.

Deploy still resolves as a member first, so someone outside the
organization gets the 404 an unknown app gets rather than learning it
exists; the source's own requirement is checked after.

No source builds yet, and `TestBuildingSourcesNeedAnAdmin` fails the
moment one does — so its role is a decision someone made.

## Pulling from someone else's registry

`internal/extregistry` holds the logins. They belong to the
**organization**, not the app: one DigitalOcean or ECR login covers every
image in it, and rotating a password should be one edit rather than one
per app. One per host per organization, or "which one does this pull
use" has no answer.

Matching is by host, and the two sides have to agree about spelling —
`NormalizeHost` reduces what someone types, `HostOf` reads what an image
reference carries, and both land on `index.docker.io` for a reference
with no registry in it at all.

**Passwords are stored as given and never returned.** A hash cannot be
sent to a registry, so it is stored plainly; an endpoint that handed it
back would turn every read of the list into a way out for it. Rotation
replaces the login and keeps the host — re-pointing in place would
silently send an app's pulls somewhere else.

A missing credential is not an error. Public images need none, and
letting the registry be the one to refuse is what keeps a deploy that
would have worked from being blocked on a guess.

`ImageSource.Resolve` returns an `app.Image` — a reference and the
credentials for it — because that is one answer. Resolving the reference
is what determines which registry is involved, and so which login
applies.

There are no MCP tools for any of this, deliberately: creating a
credential means a registry password passing through a model's context.

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
