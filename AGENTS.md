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
  user/         identities, the API keys they authenticate with, and the
                one authorization question on the instance
  project/      projects and the environments inside them
  app/          apps, deployments, and the deploy orchestrator
  registry/     who may docker push/pull, and the push webhook
  extregistry/  logins for registries Cubeship does not run
  github/       the GitHub App: private clones, and deploy on push
  setup/        the first-run flow that claims an instance
  settings/     the instance's domain and contact address
  web/          proxies page requests to the dashboard's container
  server/       mounts every module on the HTTP mux and the MCP endpoint
  platform/     infrastructure: database, dockerx, traefik, bootstrap,
                buildkit, config, authkey, regauth, httpx
  envvar/ slug/ small shared vocabulary
cmd/cubeshipd/  the daemon
cmd/cubeship/   the CLI (cobra), one file per noun
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

Dependencies run one way: `user ← project ← app`, with `registry` and
`server` on top. `server` is the only package that knows every module
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

`web/` is a Next.js app with `output: "standalone"`, built by
`Dockerfile.web` into its own image and run as `cubeship-frontend`, a
sibling of the daemon on the `cubeship` network.

**The daemon is the only thing in front of it.** It answers `/api`
itself and proxies everything else to that container, so an instance is
still **one address**: Cubeship is installed with one command and
reached by IP before there is a domain, and a dashboard on a second port
would be a second thing to open, a second thing to firewall, and a
session cookie set on one would not be sent to the other. Nothing
publishes the container's port.

It used to be a static export compiled into the daemon. That bought one
binary at the cost of every route being static — no `[dynamic]`
segments, so whatever identified a resource travelled in the query
string. Four levels deep that stopped being a constraint worth paying,
and the product was bending around a build flag. **Write real dynamic
segments.** Nothing carries an identity in the query string any more.

`internal/web` is the proxy, and the one thing it does beyond proxying
is explain itself: a dashboard container that is not up answers 502 with
the container's name and the fact that the API is unaffected, rather
than a bare gateway error at the address the installer told you to open.

`make web-dev` runs the dashboard on :3001 with hot reload, and a daemon
on the host proxies there rather than to a container —
`bootstrap.FrontendAddress` is the one place that branches. Reaching
:3001 directly works too; it rewrites `/api` to the daemon.

Package manager is **pnpm** (`pnpm-lock.yaml` is the lockfile the image
build installs from, along with `pnpm-workspace.yaml`, which carries the
settings that install is governed by), and linting and formatting are both **Biome** —
`pnpm lint` checks, `pnpm format` writes. There is no ESLint and no
Prettier.

### Two layers, and the sidebar says so

The sidebar is in sections, because its entries are not peers:

- **Workspace** — projects, environments, apps. What you deploy.
- **Platform** — registries, Git providers, DNS providers, the
  instance's own domain and credentials. What the instance is wired to. Nothing in it belongs
  to a project, and almost none of it is touched twice: a registry is
  connected once and deployed through for a year.
- **You** — the account.

Flat, those read as one list of peers, and "Registries" sat beside
"Projects" as though choosing between them were a normal thing to do.
A new module goes in whichever section it belongs to; if that is not
obvious, it is worth deciding before writing it.

The **URLs are unchanged** — `/registries`, `/dns`, `/settings` stay
where they are. The layer is a fact about what a thing is, and moving
every platform page under a `/platform` prefix would break links people
already have for no gain the sidebar does not give.

### How the dashboard is navigated

Four levels, and only the first is in the sidebar:

```
project        /                                  the grid you land on
  environment  /projects/<project>/<env>          tabs inside a project
    app        /projects/<project>/<env>/<app>
```

The URL **is** the app's reference, and the project's and the
environment's are its prefixes. Everything lives under `/projects`
because an app only means something inside an environment — a top-level
`/apps` would be a section for something that has no meaning on its own.
`/projects/<org>/<project>` redirects to `production` rather than being
a screen of its own, so there is one page for "a project's apps" instead
of two that have to stay identical.

`settings` is refused as a slug for any of them (`slug.Reserved`).
Next.js resolves a static segment before a dynamic one, so an app
actually called `settings` would be a resource nothing could open — the
settings screen would answer at its address instead, silently. Refusing
the name at creation is the only place that can be caught while the
person who typed it is still there.

There is **no flat list of apps**. An app only means something inside an
environment — `gateway` is unique in `acme/api/production` and nowhere
else — so a page that listed every app on the instance was listing
things whose names do not identify them. A project opens on
`production`, the environment it always has and cannot lose.

DNS and registries follow the same shape:

```
/dns                            the credentials
/dns/[id]                       one credential's zones
/dns/[id]/settings              its label, its secret, deleting it
/dns/[id]/zones/[zone]          a zone's records, by domain name

/registries                     the logins, Cubeship's own first
/registries/[id]                what one holds
/registries/[id]/settings       its login, deleting it
```

`cubeship` is the reserved id for the registry this instance runs.
Every other id is a stored credential's, which is a number, so the two
cannot collide — and a link to Cubeship's own registry is a name rather
than a blank.

A zone is addressed by its **name**, not by the provider's id for it: a
name is what someone recognises in a link they were sent, and it is
unique within an account either way. The id is what every API call
needs, so it is resolved from the zone listing on arrival — one extra
request in exchange for a URL that says what it is. A credential keeps
its numeric id, because its label is a thing its owner renames.

A project and an environment each have a **settings screen** (`/projects/<org>/<project>/settings`)
rather than a delete button in its header: renaming it, describing it and
destroying it are not the same kind of act, and the last one belongs at
the bottom of a page you went to on purpose. An environment's screen is
the same three sections, reached from the tab row inside a project.
`production` is the one row whose delete button is disabled rather than
absent — the reason it cannot go is worth reading, and a missing button
explains nothing. `ConfirmDialog` guards every
irreversible action by making you type the thing's own name — a second
button is no obstacle to a misclick, and the dangerous case is precisely
the one the daemon would happily carry out.

**Nothing has a display name. A slug is a name.** Creating anything asks
for a slug and nothing else, and that is all there is afterwards.

An app was always this way — its name *is* its slug, the last component
of its reference — and projects and environments having a
second, editable name made the rule an exception rather than the rule:
two ideas for one thing, asked for at creation, drifting apart after.

The derivation is what settled it. `slug.Title` turned `public-api` into
`Public Api`, deliberately dumb because anything cleverer would be a
dictionary — so the name was, almost always, the slug spelled worse.

What survives is the **description**, which says something a name never
could. `name` is gone from every create body, every PATCH, every MCP
tool and the CLI, where the positional argument is now the slug.

**No slug is editable after its resource exists** — project,
environment or app. Every one of them is a path component of an
app's registry reference, and that reference is derived on read rather
than stored, so renaming any of them would silently move every app
underneath: pushes configured against the old path would start failing,
and images already pushed would be stranded where nothing looks for them
again and no garbage collection reclaims them. The identifier is the one
promise the daemon makes to whatever is configured against it.

A name and a description are editable; the slug is shown beside them,
read-only, with that reason. `PATCH` accepts no `slug` field at any
level, so the rule holds for the API and the MCP tools too — not just
for the screen.

### An app is created empty

Creating an app asks for a slug and a description, in a modal, like
every other resource. It arrives with **no domain and nothing chosen**,
and `Orchestrator.Start` refuses to deploy it: Traefik routes by host,
so an app without one would come up answering nothing.

That is the point rather than an omission. Where an app is served has to
resolve to this host, and where its image comes from decides whether
this instance executes a repository — two decisions with consequences,
made in the app's settings with the reasons in front of you, not guessed
at in the moment you name it. `PATCH /apps/{ref}` is what makes an app
deployable, and it is the only place any of it can be changed.

The source and its settings are one field group there, never four
independent ones: `checkOrigin` judges them together, and moving an app
onto a source that builds re-checks the role against the source being
moved *to*, because that is the decision being made.

### Two sources, not four

The daemon has four: `registry`, `external`, `dockerfile`, `railpack`.
The dashboard shows **two**, because there are only two things an app
can be — something this instance builds, or something someone else
already built — and the four are two answers to a second question:

```
GitHub          ─┬─ Railpack     railpack
                 └─ Dockerfile   dockerfile
Docker image    ─┬─ Cubeship's registry   registry
                 └─ Another registry      external
```

Flattening them into one list of four put "how it is built" beside "what
it is", and made the choice that decides whether a `docker push` deploys
the app look like a peer of the choice between two build tools.

The form mirrors the daemon's `checkOrigin` refusals inline — a tag on
an external image, an `ssh://` repository, a `#ref` in the URL — so a
mistake is a sentence under the field rather than a rejected submit. It
is a courtesy, not the rule: the daemon still checks, and it is the one
that decides.

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
with `next/font/local`: the image build already needs the network for
`pnpm install`, and a second place a build can fail is one too many.

**Everything a field looks like is decided in `globals.css` and nowhere
else** — face, surface and focus, in one unlayered block. Every field is
mono, because what goes in one here is read character by character.

Unlayered is the load-bearing part. A Tailwind utility beats an `@layer
components` rule whatever its specificity, and the shadcn primitives
ship their own conflicting classes: `bg-transparent` and
`dark:bg-input/30` on Input, `ring-3` on its focus state, neither on
Textarea. Layered, the house style lost, and a text box and a text area
in the same form came out different colours with different focus
weights. The cost is deliberate: a per-usage `bg-*`, `font-*` or focus
ring on a field no longer takes.

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
pulls the images and runs the daemon. **The release is two images** —
the daemon and the dashboard, at the same version — and nothing else has
to be hosted anywhere. An upgrade is still a pull.

The daemon starts the dashboard's container, so it has to know which
image: `CUBESHIP_WEB_IMAGE`. It is *told* rather than deriving it from
its own reference, because deriving means string surgery on a registry
path an operator is free to change, and getting it wrong is an instance
whose dashboard silently never starts. The daemon's image bakes in the
matching published version as the default; `install.sh` overrides it
with `--local`, where neither image is published.

`uninstall.sh` is its counterpart, and the default is **not** the
destructive one: it removes the containers and leaves the data
directory, because someone removing the software is not thereby asking
to lose their database — installing again brings the same instance back.
`--purge` is the other thing. It lists what goes before it asks, and the
answer is the word "delete": a second button is no obstacle to a
misclick. With no terminal to ask on — which is what piping into a shell
means — it refuses rather than proceeding on silence, unless `--yes`
says otherwise.

`make test-uninstall` runs it on a Linux with Docker stubbed, including
the confirmation under a real pseudo-terminal. A destructive script that
is wrong is the worst kind.

`--local` builds both images from the checkout the script is in instead
of pulling, which is how you run unpublished code on a box: push, pull
on the server, install. The dashboard is built first and the daemon
last, because the daemon is what starts everything and a half-built pair
is better discovered before anything is replaced. It refuses when there
is no checkout beside it — piped from curl there is nothing to build,
and saying so beats a build that fails on a missing Dockerfile.

**The daemon is a container**, a sibling of Postgres, the registry,
Traefik, BuildKit, the dashboard and every app on the `cubeship`
network. Each finds the others by container name.

**`config.InContainer` is what decides every address**, and it is set in
the image rather than by whoever runs it. A daemon on the host is still
supported — that is what `make dev` runs — and reaches the same things
over loopback, with containers reaching back through
`host.docker.internal`. `bootstrap.PostgresDSN`, `LocalRegistryAddress`,
`DaemonAddress` and `FrontendAddress` are the four places that branch, and
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
projects, environments, apps. It leaves out the daemon's
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

An account can exist with no password. One an admin creates gets an API
key immediately and a password only when it sets one, which
is why every sign-in failure — unknown username, wrong password, no
password at all — is the same answer, and why an unknown username still
pays for a hash verification.

## Claiming an instance

`internal/setup` is the first-run flow, and it exists because the daemon
starts with no account at all. `POST /setup` creates the account, signs
the caller in, and closes setup permanently: `Needed` is "are there zero
users", so the *first* account is the only one setup ever makes, and it
is an admin.

That check and the insert are one transaction behind
`pg_advisory_xact_lock`, because two people opening the page at once must
not both succeed and the username's unique index would not stop them —
they may well pick different names. The loser gets `ErrAlreadySetUp`
(409).

**Nothing else is created.** There is no organization to invent any more,
and a project is something you make when you have something to put in it
— a slug is permanent, so a name picked on someone's behalf is one they
are stuck with. The projects screen opens empty, saying so and offering
the button.

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

## Where an app answers, and on which port

An app is served at any number of names, and **each name carries its own
port**: `app_domains` is `(host, port)`, not a domain column on the app.

The pair is the unit because "which port does this app listen on" stops
having one answer as soon as the app has more than one name. An image
can expose several, and `api.example.com` and `admin.example.com` on one
container are two of them.

That is also why Traefik gets **one router and one service per domain**
rather than a single `Host(a) || Host(b)` rule. A router has one
service, so every name behind one rule would reach the same port.

`host` is unique across the instance, not per app. Traefik routes by
host and nothing else; two apps claiming one name would give it two
answers, and which it picked would be a detail of label ordering.

**Port 0 means "read it from the image"**, and is the normal answer.
`EXPOSE` ends up in an image's config, so `dockerx.ExposedPorts` reads
it where it already is — the Dockerfile is not always around, and never
is for an image someone else built. An image exposing nothing has no
answer and one exposing several has no *single* answer, so both fall
back to `DefaultPort`; a number on the domain is an operator overruling
that.

The image is inspected inside the deploy, after `Resolve`, because that
is the first moment it certainly exists: an app that builds has no image
until its first build. Nothing can detect a port when the app is
configured, which is why the field is always offered.

A container keeps the labels it was created with, so adding or removing
a name changes nothing until the app is redeployed.

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

Both builds autodeploy once this instance has connected the GitHub
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
credentials are settings. The App is installed on the GitHub accounts
whose repositories this instance may build, and an installation is a row
here — the instance builds what it has been given access to, and nothing
else.

**The App is public, and connecting an installation is verified.** The
two go together. A private GitHub App can only be installed on the
account that owns it, so an instance whose App was private could reach
one person's repositories and no GitHub organization's — the install page
offered none at all. Public fixes that and costs the guarantee that came
with it: anyone can install it, so an installation id is a number the
caller chose and every id is somebody's real id.

`request_oauth_on_install` is what pays for it. GitHub sends the
installer back with a code as well as an id; `Connect` spends the code,
asks GitHub which installations *that person* administers, and refuses
an id outside the answer. The account is read from the same answer
rather than from the request — it is what every repository lookup
matches against, and a mismatched one would silently stop matching.
Turning the OAuth off while leaving the App public would make
connecting an installation a way to read a stranger's private code.

**A delivery has to name an installation this instance connected.** The
signature already stops a forgery; the lookup stops a genuine delivery
from an App installation nobody here asked for.

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

Deploy resolves as a member first and checks the source's own
requirement after, so a member deploying a building app is told they lack
the role rather than that the app is missing.

No source builds yet, and `TestBuildingSourcesNeedAnAdmin` fails the
moment one does — so its role is a decision someone made.

## Pulling from someone else's registry

`internal/extregistry` holds the logins. They belong to the
**instance**, not the app: one DigitalOcean or ECR login covers every
image on it, and rotating a password should be one edit rather than one
per app. One per host, or "which one does this pull use" has no answer.

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

**Deleting something takes everything under it.** A project takes its
environments and apps; an environment takes its apps. Every app's container is stopped and
removed on the way.

It used to refuse at each level instead, and that was bookkeeping rather
than a safeguard: reaching a project you wanted gone meant deleting its
apps one at a time first, and the daemon would have carried out every one
of those deletes anyway. The safeguard is the confirmation in front of it
— `ConfirmDialog` asks for the thing's own name, and the CLI wants
`--yes` — not a refusal you can satisfy by hand.

`production` is the one refusal left. It is created with its project and
every app assumes it exists, so it goes when the project does and never
before.

The order is containers first, then rows, and outside the transaction —
Docker has no rollback. A failure there leaves the apps gone and the
thing above them still standing, which a retry finishes; the reverse
would leave a container running with nothing on the instance naming it.
`project.AppTeardown` is the seam: `project` sits below `app` and cannot
import it, so `server` hands the app service back up at wiring time, and
a service with no teardown wired refuses to delete at all.

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
HTTP. Docker is always faked.

**Nothing local touches infrastructure.** `make check` needs no Docker,
no Postgres and no network. Standing that up to find out whether a
function returns the right string is time taken out of the edit-run loop,
and CI is where nobody is waiting.

Two mechanisms draw the line, and they are different on purpose:

- **A build tag, for anything that boots a container.** Those tests live
  in `test/integration` and are not in `./...` at all — a laptop does not
  even compile them. `internal/platform/buildkit` keeps only what needs
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
