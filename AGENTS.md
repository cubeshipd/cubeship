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
  datastore/    the databases the instance runs, and which apps are
                wired to which
  metrics/      what every container is using, sampled on a timer — the
                series both apps and databases are charted from
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

**A page's params come from Next's generated route types**, not from a
hand-written one: `PageProps<"/projects/[project]/[env]/[app]">` reads
the segments the directory actually has, so a segment renamed without
the page following it is a type error. A hand-written type is how a page
went on destructuring `org` after organizations were removed and asked
the daemon for `/apps/undefined/<project>/<env>/<app>` — which answers
"no such endpoint", at the address the dashboard sends you to after
creating something. Those generated types do not exist in a fresh
checkout, so `pnpm typecheck` writes them first; CI runs that before the
build.

Package manager is **pnpm** (`pnpm-lock.yaml` is the lockfile the image
build installs from, along with `pnpm-workspace.yaml`, which carries the
settings that install is governed by), and linting and formatting are both **Biome** —
`pnpm lint` checks, `pnpm format` writes. There is no ESLint and no
Prettier.

### Two layers, and the sidebar says so

The sidebar is in sections, because its entries are not peers:

- **Workspace** — projects, environments, apps. What you deploy.
- **Platform** — credentials, registries, Git providers, DNS providers,
  certificates, the firewall, the instance's own domain. What the instance is wired to. Nothing in
  it belongs to a project, and almost none of it is touched twice: a
  registry is connected once and deployed through for a year.
  **Credentials is first**, because the others stand on it.
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
/credentials                    the secrets, and where one is renamed
                                or rotated

/dns                            the providers, and which secret each
                                writes through
/dns/[id]                       one provider's zones
/dns/[id]/zones/[zone]          a zone's records, by domain name

/registries                     the logins, Cubeship's own first
/registries/[id]                what one holds
/registries/[id]/settings       which credential it logs in as,
                                deleting it
```

`/dns` has no `[id]/settings`, and that is the model showing through:
**a DNS provider has almost no configuration of its own.** It is which
API to speak and which stored credential to speak it with, so `/dns`
lists `GET /dns` and adds, re-points and removes rows in place.

Adding one asks two questions — the provider, and the credential — and
the second offers "type a new one", so a first provider takes no trip to
the Credentials screen. It does **not** send you there to make one
first. For one release it did, and that was the whole idea upside down:
see below.

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
the one the daemon would happily carry out. **Anything that deletes or
unlinks goes through it**, wherever it lives: a table's trash icon, a
"revoke" beside an API key, unpublishing a database's host port.

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
and it deploys anyway.

**An app with no domain is a normal app**, not a half-finished one. A
worker, a queue consumer, a service its neighbours call by container
name — none of those should answer on the internet, and an instance that
could only run things that do would be the wrong instance.
`traefik.Labels` already says so: with no domains it emits the network
label and no `traefik.enable`, so Traefik is given no opinion rather than
an empty rule. `Orchestrator.Start` used to refuse the deploy, which was
this rule read backwards.

Where an app is served is still a decision with consequences, made in
the app's settings rather than guessed at in the moment you name it, and
`PATCH /apps/{ref}` is the only place any of it can be changed.

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
`ValueCard`, `RowActions`, `AuthLayout`. A page composes those and
reaches for `ui/` directly for the rest. **A page should not restyle a
primitive** — if two pages need the same thing to look the same, it
belongs in `src/components/`.

`RowActions` is the one at the end of a table row, and it exists
because the buttons were being written out per page and the difference
showed: some lit up under the pointer and some did not, which reads as
"this one is not a button". The hover is the whole affordance — an icon
on its own says nothing about being pressable — so it is decided there
rather than by whoever writes the next table. `danger` is the only
colour a row action spends, which is what makes it mean something when
it appears.

**A page title carries no paragraph under it.** `PageHeader` has no
`sub`: a title says what you are looking at, the screen itself is what
explains it, and a paragraph repeated on every visit is a paragraph
nobody reads twice. `SectionHeader` keeps one, because a section's
subtitle is about the specific thing under it.

**A choice between named things is a select**, not a grid of cards —
which provider, which engine, which credential. `OptionCards` is for
the case it was built for and no other: where picking wrong is
expensive and the difference is a sentence rather than a word, which is
where an app's image comes from and how it gets built. Everywhere else
the cards were a paragraph per option in a dialog nobody reads twice.

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

Scalar is loaded from a CDN, **pinned and with an integrity hash**. The
version says which file to ask for; the hash says what the file is, and
they are not the same promise — a CDN serving something else under that
version would run its code on this daemon's own origin, beside the
session cookie. `scalarIntegrity` is how to change it. The page's CSP
allows no inline script: it has none of its own, and Scalar renders
without one.

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

**A cookie is not enough on its own for a state-changing request.**
`SameSite=Lax` is the first line and does not reach far enough: an app
deployed here answers at `app.example.com` while the dashboard is at
`example.com`, so the two are *same-site* and the cookie is sent —
anyone who can push an app could otherwise host a page that acts as
whoever visits it. So the session branch of the middleware requires
`httpx.SameOrigin`: `Sec-Fetch-Site` where the browser sends it, `Origin`
compared by host otherwise, neither being a refusal. `httpx.DecodeJSON`
is the other half — a body must be declared as JSON, because
form-encoded, multipart and `text/plain` are exactly the three a browser
will send cross-site without a preflight. Safe methods and API keys are
untouched: a key is not a credential a browser attaches by itself.

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

**An account's credentials can be revoked by an admin, and so can the
account.** `DELETE /users/{username}/credentials` ends every session and
revokes every API key one account holds and leaves the account — the
answer to a laptop that walked off, where what was on the machine has to
stop working everywhere at once. The password is not touched: it is a
secret in somebody's head rather than a credential lying on the machine
that was lost. `DELETE /users/{username}` is the person leaving: the
account goes and its keys and sessions go with it, in one transaction,
so nothing that authenticates outlives the account it belonged to. Two
refusals: the account you are signed in as, and the last admin — setup
closed when the first account appeared, and nothing in the API can make
an admin without one.

**Revoking a key is never refused, including the last one.** It used to
be, to stop somebody locking themselves out — and that is the wrong
trade the moment you name the case revocation exists for. A key that has
leaked has to be able to go *now*; under the old rule the answer to
"this key is in somebody else's hands" was to mint a second one first,
which leaves the leaked one live for as long as that takes and is a
strange thing to be made to do in a hurry.

What replaces it is knowing rather than refusing, which is the same
shape as every other irreversible act here: `/users/me` carries
`has_password`, so the account screen can say what revoking this one
costs — the CLI until you make another, or the way in — and ask. A
confirmation in front of it, not a refusal to work around.

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

**Claiming it takes the setup token**, which the daemon writes to
`setup-token` in the data directory on its first start and the installer
prints. Without it the installer publishes a port and whoever reaches it
first is the admin of the machine — a race the operator can lose between
running the install command and opening their browser. The data
directory is root-only, so the token makes claiming the instance take
access to the host, which is what it always meant to require.
`setup.EnsureToken` keeps the one it wrote across restarts — a new token
every start would invalidate the one the installer printed — and removes
it the moment setup succeeds, because a credential that can no longer do
anything should not be left in a directory that gets backed up.

The account gets a **password and no API key** — its way in is the
session setup starts. A key nobody is ever shown would be a live
credential lying around for nothing; keys are self-service.

## Instance settings

The domain and the Let's Encrypt contact address (optional: an account
opens without one, so TLS follows the domain alone) are rows in `settings`,
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

**Traefik's version is pinned, and the pin is load-bearing.** Its Docker
provider asks the Engine for a fixed API version, and up to v3.5 that
version is 1.24 — which Docker Engine 28 and later refuse outright.
Refused, not degraded: the provider retries forever and Traefik sees
**no container at all**, so every router that comes from a container
label silently does not exist.

Nothing about that looks broken from the outside. Apps deploy,
containers run, and the *file* provider carries on — so the daemon's own
name still routes and still gets a certificate, while everything routed
by a label answers with Traefik's default self-signed certificate. It
reads as a certificate problem, and it is a proxy that cannot see
anything. `TraefikImage` is v3.6, which negotiates the version;
`DOCKER_API_VERSION` is not a way out, because the older provider
ignores it.

`internal/certificates` carries the provider's own error into
`traefik_says` for exactly this reason — it is the only place the real
cause is written down, and it says nothing about ACME.

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

**An app is offered a name under the instance's own domain.**
`SuggestedHostFor` builds `<app>.<environment>.<project>.<instance
domain>` and the app's response carries it as `suggested_host`. Nothing
assigns it — see above — but giving an app an address used to mean
owning a domain, pointing a record at this host, and waiting for it. A
default install's domain is an sslip.io address, and *every* name under
one of those resolves to the same host with nothing registered
anywhere, so under one the suggestion is a name that works the moment it
is added. `settings.ResolvesEveryName` is what knows the difference, and
`wildcard_domain` in the settings response is what tells the dashboard
whether to also write a record. Each name is still its own Let's Encrypt
certificate; a wildcard would need DNS-01, which an IP-embedding service
has no API for.

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

## Certificates

`internal/certificates` reports what this instance holds and what it is
missing. It **issues nothing**: Traefik does that, through the ACME
resolver it is started with, and keeps the result in
`<data dir>/letsencrypt/acme.json` — the whole store, with no API in
front of it and no row about it anywhere here.

The daemon can read that file because the data directory is mounted at
the same path inside and out, which is the same reason everything else
about that directory works. It decodes each entry's PEM, takes the
leaf's own facts — names, issuer, validity, serial — and never touches
the private key sitting beside it.

What the store cannot answer is what the page is for. `reconcile` lines
the certificates up against every name this instance routes (an app's
domains, plus the instance's own two) and says which app each one
serves, which certificate serves nothing any more, and why a name that
should have one does not: no domain on the instance at all, a name added
after the app's last deploy — a container keeps the labels it was
created with — or Traefik knowing the name and not having got one yet.
That last case quotes the ACME error from `docker logs
cubeship-traefik`, because it appears nowhere else; a log line that stops
matching leaves the field empty and the report is still the report. The
complaints that name no host are carried too, in `traefik_says`: the
commonest failure on a default install is a rate limit, and Let's
Encrypt counts every name under `sslip.io` against one weekly allowance
shared with everyone else using it, because `sslip.io` is not on the
Public Suffix List.

**"Routed" is checked, not assumed.** The daemon's own name comes from
the dynamic file the daemon writes, so it is there whenever there is a
domain; the registry's comes from its container's labels, and a
container keeps the labels it was created with. One made before the
domain existed carries no router at all, so Traefik has never heard of
the name and `pending` would be a lie — the report inspects the
container and says `not_deployed` instead.

**It is read-only on purpose.** Renewing or deleting means editing a file
Traefik owns while it runs, which is only safe with the container
stopped — a few seconds of downtime for every app — and every re-issue
spends one of a weekly limit shared with everyone else using the same
registered domain. That is a decision to make deliberately, not a button
beside a table.

## The firewall

`internal/firewall` is the host's UFW: whether it is on, what it admits,
and one thing that is not UFW's at all. It **owns no rows** — the rules
live in UFW, where an operator's own `ufw` command looks for them, and a
second copy here would be a copy that drifts the first time somebody
types `ufw allow` over SSH. Same shape as `certificates`, which reads
Traefik's store.

**Docker publishes ports around UFW, and that is the whole design.** A
published port is DNAT'd and *forwarded* to a container rather than
delivered to the host, so it never passes the INPUT chain `ufw allow`
and `ufw deny` govern — and every port Cubeship opens is one of those:
Traefik's 80 and 443, an exposed datastore's, the daemon's own. A screen
wrapping `ufw status` would therefore show a firewall that is not in
front of anything you deployed, which is worse than showing nothing.

So a rule has a **scope**. `host` is `ufw allow`, traffic to the machine.
`apps` is `ufw route allow`, traffic forwarded to a container — and it
only means anything once `AdoptDocker` has appended a stanza to the
host's `/etc/ufw/after.rules` sending Docker's `DOCKER-USER` chain
through UFW's forward chain first. `DOCKER-USER` is the one seam Docker
leaves and never rewrites. Until that stanza is there, an `apps` rule is
**refused rather than written**, because a rule that governs nothing is
the exact lie this module exists to avoid.

Three refusals are the point of the module, and each is a thing that
silently costs somebody a machine:

- **Enabling with nothing admitting SSH.** UFW denies incoming by
  default, so that ends the session it was asked from and every future
  one, to somebody who is by then unable to undo it. The SSH port is
  read from the host (`sshd -T`) rather than assumed to be 22 — a
  hard-coded 22 would *pass* on a host listening on 2222, which is worse
  than no check.
- **An `apps` rule before adoption**, above.
- **Deleting by a position that has moved.** UFW deletes by number and
  numbers shift; the caller sends the rule's own text and the daemon
  refuses if it no longer matches. Otherwise a stale screen deletes a
  different rule and the only sign is a port that stops answering.

Adopting is the dangerous direction, and the **order is not cosmetic**:
the `ufw route allow` rules go in first, while they are inert, and the
stanza that starts denying goes in last. 80 and 443 are allowed whatever
the caller asked for — they are Traefik, which is every app and the
dashboard the button was pressed from. Everything else currently
published is offered, because `Status` reports it: on this kind of host
what is exposed is what containers publish, not what the host's own
services listen on.

There are **no MCP tools**, deliberately. The line is the one
`extregistry` draws by having none at all, for a different reason: an
agent that closes 443 takes the instance off the internet, and nothing
about that is worth automating.

### Reaching the host at all

The daemon is a container on a bridge network; `ufw` is a host program
editing the host's netfilter tables. `internal/platform/hostexec` is the
bridge: a throwaway container in the host's PID namespace, privileged,
running `nsenter -t 1 -m -u -i -n -p --` against PID 1. What runs is
then the host's own binary with the host's filesystem and network.

**It adds no privilege the daemon does not already hold.** The daemon
has `/var/run/docker.sock`, which is root on the host by another name —
anything that can create containers can create a privileged one. This is
that existing door, used deliberately in one place, rather than left for
a future module to reinvent worse.

The image is the daemon's own, read back from the Engine
(`bootstrap.OwnImage`) rather than configured: it is Alpine, busybox
carries `nsenter`, and it is on the box by definition — so there is no
third image in the release and nothing to pull the first time a rule is
written. It is off entirely when the daemon is a host process, which is
`make dev`, where it would be editing the developer's own firewall.

Nothing user-supplied is ever interpolated into a command line.
`Spec.Check` matches every field against a pattern — a port is digits or
a range, a source is an address, a comment is a short line of ordinary
characters — and `Spec.Args` builds argv rather than a string. The
stanza reaches the host through the **data directory**, not through an
argument: it is mounted at the same path inside and out, so the host
reads a file the daemon just wrote and several hundred bytes of iptables
syntax never pass a shell.

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

**Registering the App carries a nonce, and the exchange requires it.**
GitHub's manifest conversion endpoint is unauthenticated — a code is a
code, whoever made the manifest it came from — and the redirect that
brings one back is a link a browser follows with the session cookie
attached. So `POST /settings/github/manifest/state` issues a single-use
`state` bound to the caller, the manifest form carries it to GitHub,
GitHub echoes it back, and `RegisterFromManifest` refuses a code that
arrives without it. Without that, a link sent to a signed-in admin would
make this instance somebody else's App: their webhook secret, their
private key, and installation tokens over every repository the admin then
granted it — landing them, meanwhile, on exactly the install page they
expected. The nonce also carries whether the registration may **replace**
an App the instance already has, decided before GitHub is involved
rather than read from the redirect coming back, because replacing one
breaks every installation on it.

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

It binds creating an app, deploying one, **and writing its environment**.
A member who could create an app they can never deploy would be an odd
thing to allow — and one who could write a building app's environment
would be building through it. For an app that builds, the environment is
build input as well as the container's: Railpack reads it to work out how
to build the repository and turns `RAILPACK_INSTALL_CMD`,
`RAILPACK_BUILD_CMD` and `RAILPACK_START_CMD` into commands the build
runs. The app's own variables win the merge over its environment's and
its project's, both of which are already an admin's, so the app level was
the way round them. Reading stays a member's: seeing how an app is
configured is not deciding what it builds.

Deploy resolves as a member first and checks the source's own
requirement after, so a member deploying a building app is told they lack
the role rather than that the app is missing.

Every source is listed in `TestTheRoleEachSourceNeeds`, so adding one is
a decision about its role rather than an accident.
`TestOnlyAnAdminMayCreateOrDeployABuildingApp` and
`TestOnlyAnAdminMayWriteABuildingAppsEnv` pin the two ways in.

## Credentials

`internal/credential` holds the secrets this instance is wired to — an
AWS access key, a Cloudflare token, a registry password — and it exists
because the same secret kept being entered twice. An AWS key is the same
key whether Route 53 writes a record with it or ECR is pulled from with
it, and it used to live once under DNS providers and again under
registries: two rows, two rotations, and one of them forgotten.

**A credential is a label, an optional first half and a secret, and
nothing else.** It carries no provider. It did once, and the provider
was what said which API the daemon speaks with it — which made a
credential a thing you create *per provider*: most API tokens can only
be read at the moment they are issued, so a secret filed under the one
job it may ever do has to be issued a second time for the second job.
The point of storing a secret centrally is exactly the opposite.

**Which API is spoken belongs to the use, and every use keeps a row.**
A registry names its provider (`generic`, `digitalocean`, `aws`) and the
credential it logs in with; a DNS provider names Route 53 or Cloudflare
and the credential it writes through. One credential may be named by any
number of them, which is the payoff: one AWS key, entered once, writing
records with one hand and pulling images with the other.

**A credential is a convenience, not a prerequisite.** This is the part
that is easy to get backwards, and was: every screen that uses one also
*makes* one. A registry is added with a login typed in place and the
credential is created from it — one request, one transaction, so a
secret is never left behind by a registry that turned out to be
unreachable — and it then appears under Credentials, ready to be picked
for the second registry or for DNS. `POST /registries` and `POST /dns`
therefore take either a `credential_id` or a `label`/`username`/
`password`, and refuse both at once, which has no obvious reading.

What it must not become is a gate in front of adding a registry, which
is the tail wagging the dog — nobody's first act on a fresh instance is
naming an account.

Rotating from a registry rotates **the credential**, and everything else
on it follows. A caller who wanted only that one registry to move wanted
a different credential, which is what `credential_id` is for; the screen
says which other things share it before the button is pressed.

The module knows nothing about DNS or registries. What it knows is
`Dependant`, an interface the modules that *use* credentials implement:

- `Resolve(ctx, caller, id)` is the one way to get a secret. It asks no
  question about what the secret is for, because that is not this
  module's to answer — whether a token works for a job is the job's to
  refuse.
- `UsesCredential` is each dependant's answer to "what would deleting
  this break", so `in_use_by` can say so in the listing and a delete
  that would strand something is refused with the names.

`server` is where the two halves meet: `creds.SetDependants(registries,
dnsProviders)`, at wiring time, the same seam `project.AppTeardown` uses
and for the same reason — the module that owns the rows is the only one
that can answer, and it sits above the one asking.

**A secret is stored as given and never returned.** A provider takes the
secret itself, so a hash could not be sent to one; an endpoint that
handed it back would turn every read of the list into a way out for it.
`PATCH` with no password leaves it alone, which is what makes renaming
a credential not a rotation.

Managing them is an **admin's** job, reads included: the list names what
secrets this instance holds and what stands on each, which is not
something a member needs and is exactly what somebody probing would
want.

There are no MCP tools here, deliberately — creating one means a
password passing through a model's context — and the same reasoning that
kept them off `extregistry`.

Two migrations tell the story. `00021_credentials.sql` moved the rows
in: `dns_providers` became credentials and every external registry got
one derived from its own login. `00023_credentials_are_generic.sql`
undoes the half of that which went too far — the registries get their
`provider` column back, `dns_providers` returns as a provider and a
credential id with no secret in it, and `credentials.provider` is
dropped. Both foreign keys are `ON DELETE RESTRICT`, because a
credential something stands on must not vanish underneath it.

## Pulling from someone else's registry

`internal/extregistry` says which registries Cubeship does not run,
which kind each is, and which credential logs in to each. The login
itself lives in `credentials` — one DigitalOcean or ECR account covers
every image on it, and rotating a password is one edit there rather than
one per row. One registry per host, or "which one does this pull use"
has no answer.

The **provider is the registry's own column**, not the credential's: a
credential is a secret, and the same DigitalOcean token may be reaching
two different things. A row is joined to its credential on every read,
so everything above this — the provider clients, the deploy path — still
sees one value with a provider and a login on it. None of them had to
learn where the login moved.

Matching is by host, and the two sides have to agree about spelling —
`NormalizeHost` reduces what someone types, `HostOf` reads what an image
reference carries, and both land on `index.docker.io` for a reference
with no registry in it at all.

**The host and the provider are fixed once a registry exists.**
Re-pointing one in place would silently send an app's pulls somewhere
else, and the host was derived from the provider; what can be changed is
which credential it authenticates as — a second AWS account, not a
second address — and the secret that credential holds.

The two are not the same edit and cannot be asked for together: one
moves this registry, the other moves every registry on that account.
Rotating also drops the cached ECR token minted from the key that just
changed, or pulls would go on working for hours on the old one and then
fail for no visible reason.

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

## Monitoring

`internal/metrics` records what every container on this instance is
using and answers the series a chart is drawn from. Both `app` and
`datastore` serve it, at their own addresses.

**One module, because it is one question.** An app and a datastore are
both a container with a CPU and a resident set, and the chart is the
same chart. This package knows about neither: a `Subject` is a kind, an
id and a container, and the modules that have those hand them over
through `metrics.Source`. The read endpoints live at `/apps/{ref}/metrics`
and `/datastores/{name}/metrics`, where each module has already decided
who may look — `metrics.Service` takes no caller and checks no role,
because asking twice is two answers to a question with one.

**In Postgres, because it is the only store this instance has.**
Cubeship runs on one VPS with no external services; "add Prometheus" is
not a smaller answer than a table, it is a second thing to install, run,
back up and reach. `metric_samples` has one row per container per
interval, and `kind` is what tells an app's from a database's —
deliberately not a foreign key, since there is no one table to point at.

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

**On the dashboard**, `MetricsSection` is one component for both pages
and `TimeSeries` is the chart, over **Recharts**.

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

## Managed databases

`internal/datastore` runs Postgres, MySQL, MariaDB, Redis and MongoDB
for the apps on this instance. Each is one entry in `specs` and nothing
else — adding the last two touched that map, and the service not at all.

**A database belongs to the instance, not to a project.** That is the
design decision, and everything follows from it.

It was inside an environment first, so that an app inherited its
connection string through the layering it was already in. That is true
and it was not enough: on one VPS the common shape is a single Postgres
serving several small apps, and those apps are routinely in different
projects. Owned by `web/production`, a database could not be reached
from `blog/production` at all — not because anything prevented it, but
because the model had decided in advance that it was the wrong thing to
want.

So ownership moved to the attachment, and 00019 is the migration that
moved it. A datastore exists on its own; `datastore_attachments` is the
whole of what connects it to anything, and it may cross projects and
environments freely.

**What is given up is that an environment no longer separates data by
itself.** `pg-production` and `pg-staging` are two datastores, told
apart by their names, and attaching the wrong one to the wrong app is
now possible where it used to be unrepresentable. That is a real cost,
paid for a database that can be shared — which is the reason to run one
on a box this size.

**It is a module of its own, not part of `app`.** A database has no
image to push, no source to build, no domain, no zero-downtime swap and
no deployments table. It is provisioned once and then runs.

**The name is the whole of the address.** Unique across the instance,
because it *is* the container: `cubeship-db-<slug>`, which is the host
every attached app resolves on the shared network. An app's container is
`cubeship-<project>-<env>-<name>-<nanos>`, so the two namespaces cannot
collide and a database called `api` may sit beside an app called `api`.
`engines` is the one refused name (`reservedSlugs`) — the API lists what
it can run at `/datastores/engines`, and Go's mux prefers the literal.

**The data is a host bind mount**, under `<data dir>/datastores/<id>`,
keyed by id rather than by name. Same rule as every other container
Cubeship runs: anything in a container's writable layer is destroyed the
next time its configuration changes, and changing the published port is
exactly such a change.

### How an app reaches one

By being **attached** to it. An attachment gives the app `DATABASE_URL`
and its parts, from its next deploy onwards — a container keeps the
environment it was created with, the same rule that makes adding a
domain take effect on redeploy.

The app is named by its **full reference**, because a datastore is not
inside an environment: `api` alone identifies nothing here, and two apps
called `api` in two projects may both be attached to one database.

**Two attachments collide when they name the same variables, and the
prefix is only half of that name.** The other half is the engine's
stem — `DATABASE` for the three that hold tables, `REDIS` and `MONGO`
for the two that do not (`Engine.VarStem`). A second Postgres on one app
therefore takes a `prefix` like `ANALYTICS_`, since two would be one
variable with two values; a Redis beside that Postgres takes nothing,
because `REDIS_URL` and `DATABASE_URL` cannot overwrite each other.

That is why the stem is **stored on the attachment row** and the unique
index is `(app_id, prefix, stem)`. A unique index cannot reach into
another table for the engine, and storing it is safe because an engine
is fixed for the life of a datastore. It was `(app_id, prefix)` first,
and attaching a cache to an app that already had a database was refused
for a conflict that does not exist. `PrefixTakenError` names the
variables rather than the prefix, because an app may hold several
databases and be colliding with exactly one of them.

The seam is `app.DatastoreVars`, declared in `app` and satisfied by
`datastore` — the dependency runs `app ← datastore`, and this is the one
thing that has to travel back. It is read fresh at every deploy rather
than stored on the app, so an attachment made after the last deploy is
picked up.

**Nothing below a datastore knows it exists.** Deleting a project takes
its apps and no database; the attachments to those apps go with them,
through the foreign key. A database outliving the apps that used it is
the point — it is the instance's, and deleting an app is not a decision
about anybody's data.

### What differs between engines

Four things, and each is a field on `spec` rather than a branch
somewhere:

- **How the password is delivered.** Every SQL engine takes it from the
  environment; Redis's official image has no variable for it, so it goes
  on the command line (`--requirepass`). `cmd` exists for that one case
  and `env` is nil there.
- **Whether the login is yours to choose.** Redis has one, called
  `default`, and the password belongs to it. Naming another is
  *refused*, not overwritten — silently ignoring what somebody typed is
  how a credential comes out different from what they thought they
  asked for. MySQL and MariaDB refuse `root` for the mirror reason: it
  already exists and this would not be its password.
- **Whether a named database means anything.** Redis's numbered
  databases are not the same idea and are not something to provision,
  so its variables carry no `_NAME`.
- **What the variables are called.** `stem` is `DATABASE` for the
  relational ones, `REDIS` and `MONGO` for the others — so an app with
  a Postgres *and* a Redis attached gets both, unprefixed, without
  colliding.

**None of them may move its data below the mount point.** Each of these
images chowns its data directory — and only its data directory — to the
unprivileged user it drops to. Point that at a subdirectory and the
mount above it keeps the mode the daemon created it with, `0700 root`,
which the engine's own user cannot traverse: the container comes up as
root, chowns something it can reach, drops privileges, and then cannot
open the directory it just prepared.

Postgres is where this bit. Its image documents pointing `PGDATA` at a
subdirectory when the data lives on a bind mount, and following that
advice broke every Postgres this module provisioned — `Permission
denied` on a restart loop, from a container that had been root a moment
earlier. The daemon's own Postgres never had the problem because it
never set `PGDATA`; `bootstrap.PostgresContainerOpts` is the same image
on the same kind of directory, and it works. `TestNoEngineMovesItsDataBelowTheMount`
is what keeps the advice from being taken again.

Two smaller ones worth knowing: Redis is started with `--appendonly
yes`, because otherwise it snapshots on its own schedule and a restart
loses the last few minutes — free for a cache, and somebody's queue
otherwise. And Mongo's connection string carries `authSource=admin`,
because its root user lives in the `admin` database whatever database
the connection names; without it every connection fails on credentials
that are perfectly correct.

### Turning one off

`POST /datastores/{name}/stop` stops the container and leaves it, and
its data, where they are. **Stopped rather than removed, so the log
survives** — what somebody wants immediately after turning a database
off is usually the reason they turned it off.

The status becomes `stopped`, not `down`, and the reconciler leaves it
alone: Docker's restart policy is `unless-stopped`, so it is still off
on purpose after a reboot, and rewriting that to `down` would turn a
decision into what looks like a fault on every daemon start.

`start` provisions again rather than starting the container that is
already there — one path instead of two, the data being a bind mount a
recreate does not touch. It is also how a datastore whose provisioning
failed is retried.

`has_container` on the response is what says whether there is a log to
read or anything to stop. The status cannot answer it: a datastore whose
provisioning failed may have neither.

### Fixed after creation, and why each one is

- **The name.** It is the container's, which every attached app resolves.
- **The engine and the version.** A data directory written by one major
  version is not readable by another. A datastore that "changed version"
  would be a container that will not start, with the only copy of the
  data inside the directory it will not read. Running a new version
  means a second datastore and the engine's own migration tools.
- **The password.** It is used once, when the engine initializes itself,
  and nothing reads the column afterwards. Changing it would change
  every connection string Cubeship hands out while the database went on
  accepting only the old one.

The description is what is left, which is why `PATCH` takes one field —
and with no project above it to say where a database belongs, it is the
only place that can.

### Credentials

Stored as given, like an external registry's login and for the same
reason: a hash cannot connect to anything. Never in a listing —
`GET /datastores/{name}/credentials` is its own request and an admin's.
Generated when a request carries no password, so a database with a weak
one is not something anybody gets by leaving a box empty; the dashboard
generates its own and shows it, because a field somebody has to fill in
is a field somebody fills in badly.

`Datastore.URI` builds the connection string through `net/url`, so a
chosen password containing `@` or `/` is escaped rather than producing a
URL that parses as a different host.

**The MCP tools stop short of two things**, and the line is the one
`internal/extregistry` draws by having no tools at all: no tool reads or
sets a password, and no tool exposes a datastore. An agent can provision
a database, attach an app and never hold the credential, because the app
receives it through its environment.

### Exposing one

Off by default. `POST /datastores/{name}/expose` publishes it on a host
port — from `PortRangeStart`-`PortRangeEnd` unless one is named — for a
migration run from a laptop, psql, a BI tool.

**Not through Traefik.** Traefik routes HTTP by host name; a database
speaks its own protocol on its own port, and a TCP router matching
`HostSNI(*)` can only have one backend per entrypoint, so two exposed
databases of one engine could not share one. The container publishes the
port itself.

That means there is **no TLS**, and the endpoint says so. What makes an
exposed database safe is the password and a firewall rule, and the second
is the operator's. Publishing replaces the container to pick the port up
— published ports are fixed at create time — and the data survives
because it is a bind mount.

Ports 15000-15999 for the automatic range: the daemon's own Postgres
already publishes 5432 on loopback, so the obvious number is the one
number that cannot work.

### In the dashboard

`/databases` is its own section in the sidebar, beside Projects rather
than under Platform: a database belongs to the instance, but it is a
thing you deploy against, not a thing the instance is wired to, and it
is opened as often as an app is.

The list is a **table**, like the registries and the DNS accounts: what
someone comes here to do is scan a column — which engine, is it up, what
is using it — and cards make you read each one whole to find the line
you were after.

One database's page is **sections, not tabs**: monitoring, then how to
connect, then what is connected. None of the three is an alternative to
the others, and hiding two behind a click made you click through all of
them every time. Monitoring is first because it is the question you have
before you know you have one.

The connection details are **fields with a copy button** rather than a
table of values. A connection string is long, and a field bounds it and
keeps it on one line where a wrapped paragraph gives you three and a
chance to miss one.

`CopyField` is `disabled` and `user-select: none`. It was `readOnly`
first, focusing and selecting itself on a click — so clicking its
*label* lit the focus ring and highlighted the value, which reads as an
edit about to happen on something that cannot be edited. The button is
how a value leaves the field; nothing has to take focus for that, and a
selection nobody asked for is only ever half a connection string. The
dimming a disabled control normally gets is undone for these in
`globals.css`, where every other field rule lives: dimming says
"unavailable for now", and this is not unavailable, it is final.

Settings stays on its own page like every other resource, because the
actions that cannot be undone belong at the bottom of a page you went to
on purpose. It holds two things and no more — publishing on a host port,
and deleting. There was a "general" section above them showing the name,
the engine and the login; everything in it was already on the database's
own page one click away, and a settings screen that mostly restates
another screen teaches people that settings screens are where facts
live.

### What is not here

**Backups.** "Managed" suggests they exist and they do not. The storage
layout does not preclude them — `docker exec` a dump into
`<data dir>/datastores/<id>/backups` — but nothing runs one, and
deleting a datastore deletes its data with no copy anywhere.

**Rotating a password**, for the reason above: it would take an
`ALTER USER` inside the database, not a column write.

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
