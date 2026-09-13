# Templates

A template is a set of apps and the managed data they need, described in
one YAML file. The community publishes them as GitHub repositories; the
catalog on cubeship.dev lists every release that passes the validator.
An instance consumes one; it never publishes one.

Three pieces, and each owns one thing:

| Piece | Where | Owns |
| --- | --- | --- |
| The validator | `product/template` | what a valid template is |
| The catalog | `hosted/catalog` | the indexer, its database, and the public API that is the only way to read it |
| The site | `hosted/site` | pages over that API. No database, no bucket, nothing written |

## Why GitHub, and not a registry of our own

The first version was a registry of our own: GitHub sign-in, an editor
with live diagnostics, drafts, immutable versions, likes, comments,
reports and a moderation queue, all in the site. It worked, and it was
the wrong trade. Every one of those is something GitHub already does
better — authorship, history, discussion, stars — and each one was a
table, a route, an auth check and a spam surface we had to keep.

So a template is a repository. The site holds no identity, takes no
input and reaches no database: the attack surface of a community catalog
is a job reading public repositories, a read-only API over what it read,
and pages rendering that.

## What a template may not contain

Four of these come straight out of what the daemon enforces, and they
bound what a template can ever be:

- **No domain.** A hostname is unique across the whole instance and is
  refused if it is one of the instance's own names. A literal host would
  make a template installable exactly once, on one machine. Domains come
  from inputs.
- **No machine names.** `nodes` is instance-local vocabulary. A template
  says `scale`, or says `spread` to follow the cluster.
- **No push-registry app.** An app whose image you `docker push` has no
  image until somebody pushes one. A template's apps are a public image,
  or a public repository built on the instance with Dockerfile or
  Railpack.
- **No command override**, because apps have none.
- **Volumes are paths, and only paths.** `apps[].volumes: [{ path }]`
  becomes a volume on install, one copy on one machine like any other
  (see [deploys.md](../product/deploys.md#volumes)). A volume refuses
  `scale` above 1, `spread` and `autoscale` beside it
  (`volume.one-copy`), and needs a `minCubeship` no release before
  0.7.0 satisfies (`volume.min-cubeship`). Uninstalling keeps the data
  unless asked otherwise. The published schema says the same in an
  editor, through an `if`/`then` on a pattern for `minCubeship`: a
  pattern reads simple ranges only, so it may flag one the validator
  accepts, and `volume_schema_test.go` holds it to never passing one the
  validator refuses.

Excluded for the same reason, all of it instance-level: DNS providers,
backup schedules, certificates, firewall rules, external registries, and
stored credentials. A template that could carry a credential would be a
template that could steal one.

The file itself — keys, inputs, references, every diagnostic code — is
documented for authors under `hosted/site/content/docs/templates`, and
that is the reference. What follows is why it is built the way it is.

## The validator is the product's

`product/template` is outside `internal/` on purpose: the catalog
imports it, and the daemon will when it applies a template. It cannot be
under `internal/` — Go refuses an internal import from another module,
`replace` or not. Two programs that must agree on what a valid file is
should run the same code, so the catalog's module replaces `cubeship`
with `../../product` and compiles the validator from the same checkout.

It used to be TypeScript with Zod, bundled into the editor. With no
editor there is no reason for it to be JavaScript, and every reason for
it to be the language the daemon is written in.

Four layers, each run only when the one before left something to work
with: YAML into a node tree that keeps every position; the shape, walked
by hand over that tree so an unknown key is refused with the nearest real
one as a hint; the semantics; then advice, which never refuses. A valid
file ends as `Normalized` — names filled from keys, defaults applied,
environment sorted, internal hostnames spelled out — which is what the
catalog stores and an instance will read.

Two YAML features are refused outright: a key given twice, which a
parser would quietly resolve to the second, and anchors and aliases, where
one alias can expand to more document than anybody wrote.

**The rules are copied from the daemon, and tested against it.** Engine
versions, CPU and memory floors, the autoscale bound and the health path
character set live in `internal/datastore`, `internal/limits` and
`internal/app`. Importing those would drag Docker's client into
everything that reads a template, so `values.go` and `engines.go` carry
copies and `rules_test.go` compares each with its original. A daemon
that learns Postgres 19 fails that test until templates learn it too.

`schema.json` is the published JSON Schema, served at
`/schema/template/v1.json`. It is a file, not generated, so
`TestJSONSchemaMatchesTheDecoder` checks its keys against the decoder's,
and `make reference` copies it into the site's `public/` with
`reference-check` refusing a stale copy.

## The indexer

`hosted/catalog` is one Go service with two jobs: a pass over GitHub on
start and every five minutes after (`CATALOG_INTERVAL`), and the API
below the whole time. One binary, because the pass already holds a lock
that makes several copies safe and the reads scale with copies anyway;
two services would buy nothing until there is traffic to split.
`/healthz` is the last pass, always 200, because GitHub being down is
the service working as intended and the API is still answering.

A pass:

```
search topic:cubeship-template          GraphQL, 50 repositories and their
  │                                     last 10 releases per request
  ├─ for each repository
  │    blocked owner or repository? ─── save as hidden, read nothing
  │    for each release, newest first
  │      draft, prerelease, no commit?  skip
  │      (tag, commit) already read?    skip
  │      read template.yaml, README.md, icon.png at the commit
  │      validate, check the icon ───── save accepted or rejected, with every diagnostic
  │
  └─ known repositories search did not return
       looked up by node id ─────────── gone → hidden · lost the topic → hidden · else read as above
```

Decisions worth keeping:

- **Releases, read at the commit their tag points to.** A release asset
  can be replaced after it is published; a commit cannot. The row is
  keyed by `(repository, tag, commit)`, so a tag moved to another commit
  is a new release and nothing already indexed changes under anybody.
- **Read once.** A release already recorded — accepted or rejected — is
  never read again. A rejection keeps its reasons so a site can show
  them, and so the same mistake is not re-downloaded every five minutes.
- **A failure is not a rejection.** A file GitHub did not finish
  answering about records nothing, and the next pass tries again. Only a
  definite answer — not found, too large, invalid — rejects a release,
  because only those are the author's to fix.
- **Search is not trusted to delete.** It is an index and can lag or
  drop a result, so a known repository it did not return is looked up by
  node id before it is hidden. GitHub answers an id that no longer
  resolves with a null node and a `NOT_FOUND` error beside the rest of
  the data, which the client reads as an answer, not a failure.
- **Hidden, never deleted.** `repositories.hidden` is `blocked`, `gone`
  or `untagged`, and a repository seen again with the topic is listed
  again with its history intact.
- **The newest accepted release is the listing.** `latest_release_id`
  is recomputed on every save, so a broken v2 leaves v1 listed.
- **The icon is re-encoded, and kept in Postgres.** Its header is read
  first and anything not a square PNG between 128 and 1024 pixels is
  refused before it is decompressed; what is stored is our own encoding
  of the pixels, never the author's bytes. It is a `bytea` on the
  release rather than an object in a bucket: a few hundred kilobytes a
  template, and one fewer thing the service has to be given.
- **One pass at a time.** Each pass holds a Postgres advisory lock on a
  dedicated connection; a second copy skips its turn rather than
  indexing the same release twice.

The budget: GitHub allows 5,000 GraphQL points an hour with a token, and
a pass is one request per fifty repositories plus three file reads per
*new* release. Steady state is a handful of requests every five minutes.
GitHub's search stops at 1,000 results, which is a problem worth having.

Moderation is `blocklist`, a table of owners and `owner/name` subjects
written by hand. The catalog is open; a pre-publication queue would make
one person the gate on every author's first impression.

## The API

The only way into the catalog. Read-only, JSON, CORS open, cached for a
minute — a pass writes at most every five. Its public address is
`https://cubeship.dev/api/v1`, and instances will pin it, so a route
under `/v1` never changes meaning.

| Route | What |
| --- | --- |
| `GET /v1/templates?q&tag&sort&cursor&limit` | the listing: `sort` is `recent` or `stars`, `limit` at most 48, keyset `next_cursor` |
| `GET /v1/templates/{owner}/{repo}` | one template: its listing fields, `readme`, `source`, `source_url` and the normalized `manifest` |
| `GET /v1/templates/{owner}/{repo}/releases` | the history, refused releases included with their `problems`, so an author can see why |
| `GET /v1/templates/{owner}/{repo}/manifest?release=v1.0.0` | the normalized manifest alone, the listed release's without `release`. What an instance will call |
| `GET /v1/tags` | every topic a listed template carries |
| `GET /v1/icons/{repository}/{commit}.png` | an icon, cached forever: the commit is in the address |

A failure is `{"error": {"code", "message"}}`: `invalid_query`,
`not_found`, or `unavailable` for anything the database did, with the
cause in the log and never in the response.

**cubeship.dev proxies it; it does not rewrite to it.** `proxy.ts` sends
`/api/v1/*` to `CATALOG_URL` on each request. A `rewrites()` entry in
`next.config.ts` would be evaluated at build time and baked into the
image, and the image is built on the instance before anybody has told it
where the catalog is. The site's own pages skip the proxy and call
`CATALOG_URL` on the instance's internal network.

`CATALOG_PUBLIC_URL` is what the icon addresses in a response start
with, so a browser fetches them through the public address.

`verified` marks a template whose owner is in `CATALOG_VERIFIED_OWNERS`,
`cubeshipd,lucasaarch` by default. It is decided in the API, not stored: vouching
for an owner is a setting, and changing it must not wait for a pass or
rewrite rows. It says who published it and nothing about the file,
which every listed template has passed the same validator for.

## The tables

The catalog owns them and migrates them with goose on start. Nothing
else connects to that database.

| Table | Holds |
| --- | --- |
| `repositories` | GitHub's `id` as the key, so a rename is an update; `node_id` for lookups; owner, name, description, stars, topics; `hidden`; `latest_release_id` |
| `releases` | `(repository_id, tag, commit_sha)` unique; `status` accepted or rejected; `problems` as every diagnostic; `manifest` as normalized JSON, `source`, `readme` and `icon` for an accepted one only |
| `blocklist` | `subject`, `reason` |

The registry's old tables — `users`, `sessions`, `templates`,
`template_versions`, `likes`, `comments`, `reports` — are left where
they are in the database cubeship.dev already ran. Nothing reads them;
dropping them is a separate, deliberate step.

## The pages

| Route | What is on it |
| --- | --- |
| `/templates` | search, topic filters, newest or most starred, a grid of cards: icon, name, description, owner, stars, updated |
| `/templates/{owner}/{repo}` | the README, what the file creates, the file with the raw URL at its commit, and beside it the author, stars, release, required version, tags and release history |

The name is the API's `title`, read from the repository:
`cubeship-uptime-kuma-template` is Uptime Kuma. The README is rendered on the server with GitHub's
own sanitizer allowlist after raw HTML is parsed, and its relative links
and images are resolved to the release's commit, so a page never shows a
newer screenshot than the file it describes.

**No page advertises a command that does not exist.** Until an instance
can apply a template, the detail page offers the file and the address an
instance would read it from.

## Running it

Two apps on a Cubeship instance. Only the catalog is attached to
anything:

| Variable | Site | Catalog |
| --- | --- | --- |
| `DATABASE_URL` | — | attaching the managed Postgres |
| `GITHUB_TOKEN` | — | a token with no scopes: everything it reads is public |
| `CATALOG_URL` | the catalog's internal address, `http://cubeship-cubeship-production-cubeship-catalog:8080` | — |
| `CATALOG_PUBLIC_URL` | — | `https://cubeship.dev/api/v1` by default |
| `CATALOG_INTERVAL`, `CATALOG_TOPIC`, `CATALOG_VERIFIED_OWNERS`, `PORT` | — | `5m`, `cubeship-template`, `cubeshipd,lucasaarch`, `8080` |

The catalog's image is `hosted/catalog/Dockerfile`, built from the
repository root like every other. Its database tests take a schema per
test in the Postgres `make db-up` runs and skip under `-short` like every
other DB-backed test; `make check` runs the rest. Locally, `make
catalog-db-up`, then `make catalog-dev` with a `GITHUB_TOKEN`, then `make
site-dev` — the site's default `CATALOG_URL` is `make catalog-dev`'s.

## Installing one on an instance

`product/internal/templateinstall` is the daemon's half, behind
`/api/templates` and `/api/template-installs`, the MCP tools that mirror
them, `cubeship template list|install|installed|update|uninstall`, and
the dashboard's Templates section — a Catalog tab and an Installed tab.

**The instance reads, and trusts nothing it did not check.** It lists the
catalog through `CUBESHIP_CATALOG_URL` (cubeship.dev's API by default) and
proxies icons, so a browser on the dashboard never talks to the catalog.
To install, it reads `template.yaml` from GitHub at the commit the catalog
recorded for the release — not the catalog's copy — and runs the same
`product/template` validator. A bare `minCubeship` is a minimum; one with
an operator is a range.

**A catalog on the same machine is reached without leaving it.**
cubeship.dev is an app on the instance we run, and that instance reads
its own catalog: a request for a name that points back at the machine
leaves for its public address, and a host that does not hairpin its own
NAT answers nothing — the lookup hung for twenty seconds and said the
catalog could not be reached. `platform/selfdial` resolves the name first,
and when it lands on one of the instance's own addresses — its public
address, or what its own domain resolves to — dials `cubeship-traefik` on
the same port instead. Host and SNI are unchanged, so TLS verifies against
the certificate Traefik serves for that name. No private DNS, nothing to
configure; a daemon not on the shared network falls back to the public
route.

**Any accepted release can be installed, not only the newest.**
`GET /templates/{owner}/{repo}/releases` lists them, and
`GET /templates/{owner}/{repo}/manifest?release=` reads one the way an
install reads it — from the repository at its commit, validated here — so
the dashboard's Version picker shows the form that version installs with,
and says when it needs a newer Cubeship than the instance runs.

**A project the install creates wears the template's icon.** The icon of
the release installed, not the newest one: the listing's `icon_url`
gives the repository id, and the icon is fetched by that id and the
installed commit, then handed to the project's own `SetImage`, which
checks the bytes like any upload. Only a project the install created — an
existing one keeps the picture somebody chose — and best effort: a
project with no picture wears a mark, which is no reason to undo an
install.

**Everything is checked before anything exists.** The project and
environment are taken from the request or the template, and created only
when missing. Every database, store and app name, every domain and every
answer is checked against the instance first — names free, domains not
taken, inputs valid for their type — and a refusal creates nothing.
Secrets with `generate` are made then and returned in that one response;
they are never recorded, only written into the apps that use them. The
MCP tool does not return them.

**An installation is a row, and every change to it is a run.**
`template_installs` is what is installed: the release, the normalized
manifest at that release, the answers that are not secret, and every
resource with the template key it came from. `template_install_runs` is
each install, update and uninstall — its step, its error, what it
created, and for an update what the apps looked like before. A run of its
own is what lets a failed update leave the installation exactly as it
was.

**An install runs detached, recorded as it goes:**

```
project/environment (when missing) → databases, stores → apps and their settings
  → wait for databases and stores → buckets → domains, attachments, variables
  → deploy each app and wait for it
```

References are resolved from what was created: a database's credentials,
an app's internal address, the answers. **Any failure undoes exactly the
recorded resources, newest first** — a project that already existed is
never in the list, so it is never touched.

**An update applies what changed and deletes nothing.** The preview —
`GET /template-installs/{id}/update` — compares the newer release's
manifest with the recorded one by key: a changed image or build source,
changed health, limits or scale, each variable the template declares
that is new or different, and new apps, databases, stores, buckets,
domains and attachments. What the release dropped is listed as kept and
left alone, and so is a database whose engine or version it changed,
because those are fixed once one exists. Variables somebody added by hand
are never touched; one the template declares gets the new value, and the
preview says so. A question the release adds with no default, or a secret
it needs that the installation cannot read back, is asked there. A secret
is read back only from an app the template gave it to whole
(`APP_SECRET: ${input.appSecret}`), since it is recorded nowhere else.

Applying records every app it will change as it is, then creates, changes
and deploys what is new or different. **If any step fails it puts things
back**: what the update created is deleted, the recorded apps get their
source, settings and variables back and are deployed again, and the
installation stays on its release.

**An uninstall keeps the data unless told otherwise.** It deletes the
installation's apps, then — only with `keep_data: false`, which the
dashboard makes you type the name for — its databases and stores, then the
project and environment it created once no app is left in them. Anything
it cannot delete leaves the installation installed, with the reason on
the run.

A daemon that restarts during a run finishes it on start, as the account
that started it: an install is undone, an update put back, an uninstall
carried on.

## What is left for later

Updating on its own when a release appears, moving back to an older
release on purpose, deleting what a release dropped, and installing from
a repository the catalog does not list. The
first wall authors will hit is now that apps have no command override,
and that is a daemon change.
