# Templates

A template is a set of apps and the managed data they need, described in
one YAML file. The community publishes them as GitHub repositories; the
catalog on cubeship.dev lists every release that passes the validator.
An instance consumes one; it never publishes one.

Three pieces, and each owns one thing:

| Piece | Where | Owns |
| --- | --- | --- |
| The validator | `product/template` | what a valid template is |
| The indexer | `hosted/discovery` | the catalog's tables, and everything written to them |
| The catalog | `hosted/site` | showing what the indexer accepted. It writes nothing |

## Why GitHub, and not a registry of our own

The first version was a registry of our own: GitHub sign-in, an editor
with live diagnostics, drafts, immutable versions, likes, comments,
reports and a moderation queue, all in the site. It worked, and it was
the wrong trade. Every one of those is something GitHub already does
better — authorship, history, discussion, stars — and each one was a
table, a route, an auth check and a spam surface we had to keep.

So a template is a repository. The site holds no identity, takes no
input and has no API: the attack surface of a community catalog is a
job reading public repositories and a page rendering what it read.

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
- **No volumes and no command override**, because apps have neither.
  Persistent data on this platform is a managed datastore or a bucket,
  so every app in a template is stateless.

Excluded for the same reason, all of it instance-level: DNS providers,
backup schedules, certificates, firewall rules, external registries, and
stored credentials. A template that could carry a credential would be a
template that could steal one.

The file itself — keys, inputs, references, every diagnostic code — is
documented for authors under `hosted/site/content/docs/templates`, and
that is the reference. What follows is why it is built the way it is.

## The validator is the product's

`product/template` is outside `internal/` on purpose: the indexer
imports it, and the daemon will when it applies a template. Two programs
that must agree on what a valid file is should run the same code, so the
indexer's module replaces `cubeship` with `../../product` and compiles
the validator from the same checkout.

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

`hosted/discovery` is a Go service that runs a pass on start and every
five minutes after (`DISCOVERY_INTERVAL`). It is not an API; its only
HTTP answer is its own last pass, always 200, because GitHub being down
is the service working as intended and not a container to replace.

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
- **The icon is re-encoded.** Its header is read first and anything not
  a square PNG between 128 and 1024 pixels is refused before it is
  decompressed; what is stored is our own encoding of the pixels, never
  the author's bytes. Icons go to the bucket the site already serves
  images from, under `templates/icons/<repository>/<commit>.png`.
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

## The tables

The indexer owns them and migrates them with goose on start. The site
reads them through a Drizzle description of its own and never migrates
anything.

| Table | Holds |
| --- | --- |
| `repositories` | GitHub's `id` as the key, so a rename is an update; `node_id` for lookups; owner, name, description, stars, topics; `hidden`; `latest_release_id` |
| `releases` | `(repository_id, tag, commit_sha)` unique; `status` accepted or rejected; `problems` as every diagnostic; `manifest` as normalized JSON, `source`, `readme` and `icon_key` for an accepted one only |
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

The name is read from the repository: `cubeship-uptime-kuma-template` is
shown as Uptime Kuma. The README is rendered on the server with GitHub's
own sanitizer allowlist after raw HTML is parsed, and its relative links
and images are resolved to the release's commit, so a page never shows a
newer screenshot than the file it describes.

**No page advertises a command that does not exist.** Until an instance
can apply a template, the detail page offers the file and the address an
instance would read it from.

## Running it

Both are apps on a Cubeship instance, attached to the same managed
Postgres and the same bucket, so the variables are the ones Cubeship
injects:

| Variable | Site | Indexer |
| --- | --- | --- |
| `DATABASE_URL` | reads | owns |
| `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `S3_PATH_STYLE` | reads icons | writes icons |
| `GITHUB_TOKEN` | — | a token with no scopes: everything it reads is public |
| `DISCOVERY_INTERVAL`, `DISCOVERY_TOPIC`, `PORT` | — | `5m`, `cubeship-template`, `8080` |

The indexer's image is `hosted/discovery/Dockerfile`, built from the
repository root like every other. Its database tests take a schema per
test in the Postgres `make db-up` runs, and skip under `-short` like
every other DB-backed test; `make check` runs the rest.

## What is left for later

The daemon's half: applying a normalized manifest, `cubeship template
apply <owner>/<repo>`, and a browser over the catalog in the dashboard.
The first wall authors will hit is still that apps have no volume and no
command override, and that is a daemon change.
