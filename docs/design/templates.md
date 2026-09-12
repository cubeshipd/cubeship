# Templates

A template is a published recipe for a set of apps and the managed data
they need: a photo, a name, a description and one file. The community
writes them on cubeship.dev, signed in with GitHub. An instance consumes
one; it never publishes one.

**This document is the design agreed before any of it was built.** The
registry lives entirely in `site/` — see [site.md](site.md) for what the
site was before it held state. Nothing in the daemon changes in this
pass; the half that applies a template to an instance is designed
separately, against the API described here.

## The daemon consumes, the site publishes

The split is deliberate and it is the whole shape of the feature. The
site owns identity, authorship, moderation, likes, comments and the
schema. An instance owns the machine, the secrets and every decision the
template cannot make for it.

That is also why the site is not a dependency of anybody's deploy: the
primitive on the instance is "apply a manifest", and fetching one by slug
from cubeship.dev is a thin layer over it. A template that somebody saved
to disk keeps working with the registry offline, deleted, or unreachable.

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
  so every app in a template is stateless. And two apps that differ only
  by their start command — the usual web-plus-worker pair out of one
  image — cannot be expressed at all unless the image takes its role
  from an environment variable. This is the first wall template authors
  will hit, and the fix is a daemon that accepts a command, not anything
  the site can do.

Excluded for the same reason, all of it instance-level: DNS providers,
backup schedules, certificates, firewall rules, external registries, and
stored credentials. A template that could carry a credential would be a
template that could steal one.

## The file

```yaml
version: 1
minCubeship: "0.6.0"
project: umami            # suggested slug; the installer may change it

inputs:                   # what the template cannot know
  - key: domain
    type: domain
    label: Where the dashboard answers
  - key: appSecret
    type: secret
    label: The app's session secret
    generate: 32          # the instance generates it and shows it once

databases:
  - key: db               # template-local; what references name
    name: umami-db        # suggested instance name, renamable at install
    engine: postgres
    version: "18"
    database: umami       # the password is generated, never written here

apps:
  - key: web
    name: web
    image: ghcr.io/umami-software/umami
    tag: postgresql-v2
    port: 3000
    health: /api/heartbeat
    domains:
      - host: ${input.domain}
    attach:
      - database: db      # contributes DATABASE_URL and friends
        prefix: ""
    limits: { cpu: 1, memory: 1Gi }
    scale: 1
    env:
      APP_SECRET: ${input.appSecret}
      DATABASE_TYPE: postgresql
```

### Keys and names are two different things

Every app, database and store has a `key`, which is what references use
and which never leaves the file, and a `name`, which is what appears on
the instance. The installer may change any name. It has to be able to:
datastore names are unique across an entire instance, so two installs of
one template would collide on the first one.

### References are resolved by the installer, not by us

`${input.k}`, `${app.k.internal}`, `${app.k.host}`, `${app.k.port}`,
`${db.k.host}`, `${db.k.port}`, `${db.k.user}`, `${db.k.password}`,
`${db.k.name}`, `${store.k.bucket}`, `${store.k.endpoint}`.

A generated password does not exist until the instance creates it, so the
site can only prove that a reference points at something declared and at
an attribute its kind has. The normalized JSON we serve keeps every
placeholder intact, and whatever applies the template substitutes.

One reference needs nothing from the instance and is worth understanding:
an app reaches another at `cubeship-<project>-<environment>-<app>` on
that app's own port, a Docker network alias derived from names the
template already declares. So cross-app wiring needs no new concept —
`${app.web.internal}` is a string the validator can compute and show the
author.

### Object stores

A template declares a managed store the way it declares a database, with
a `key`, a suggested `name`, a `version` and the `buckets` it should
have. It cannot declare a linked S3 endpoint, because that is somebody's
credential; a template that wants one asks for an input of type `store`
and the installer picks a store that already exists on the instance.

`attach` entries name either a `database` or a `store`, each with a
`prefix`, and a store also names the `bucket`, because the bucket lives
on the attachment rather than on the store. That is what lets one app
hold two buckets from one store at two prefixes.

### Inputs

`key`, `type`, `label`, `help`, `required`, `default`, and per type a
`pattern`, `min`, `max` or `options`. The types are `domain`, `text`,
`number`, `choice`, `secret` and `store`. A `secret` with `generate: n`
is produced by the instance and shown once; a `store` asks the installer
to pick an object store that already exists there, which is how a
template uses S3 without carrying anyone's keys.

### The file declares, it does not sequence

We serve the declaration. The consumer computes the order. A
precomputed list of API calls would bake one daemon version's surface
into the site and go stale the first time a route moves.

The order is not complicated, and the instance's own dependency chain
fixes it: project, environment, apps, domains, environment variables,
then attachments, and deploys last, because a container holds the
environment it was created with and only a new deploy sees an
attachment's variables. There is no transaction across resources on the
instance, so applying a template is an ordered sequence with a
partial-failure story, the same problem `cubeship app create --domain`
already has in miniature.

## One schema, two runtimes

The schema is defined once in TypeScript with Zod. That module is bundled
into the editor, so the author gets diagnostics as they type, and the
identical module runs on publish as the authority, because a browser is
not a place to enforce anything. A JSON Schema is generated from it and
published at `/schema/template/v1.json`, a permanent path, for editor
completion and for agents writing templates. `POST /api/v1/validate`
exposes the same module so a file can be checked before it ever reaches
the site.

Unknown keys are an error, not an ignored field, and the error suggests
the nearest real one. A silently dropped `healthcheck` is the worst
outcome available.

## The validator

Four layers. The YAML is parsed to a document that keeps a byte range per
node, which is the only way an issue becomes an underline on a line. Then
the schema. Then the semantics, which is everything the shape cannot say:

- every key unique, within its kind and across references;
- every `${...}` resolving to something declared, with an attribute that
  exists for its kind;
- a domain coming only from an input of type `domain`;
- attachment prefixes that do not collide on the variables they write,
  because two Postgres at the same prefix both claim `DATABASE_URL`;
- engine and version a pair the daemon knows: postgres 15 to 18, mysql
  8.0 and 8.4, mariadb 10.11 and 11.4, redis 7.2 and 7.4, mongodb 7.0
  and 8.0;
- Redis forcing the username `default`, MySQL and MariaDB refusing
  `root`, and a `password` field refused outright for any engine;
- CPU at least 0.01 cores and memory at least 6 MiB;
- autoscale `max` at most 100 with `min` inside `[1, max]`;
- a health path that starts with `/`, is at most 255 characters, and
  carries no `?` or `#`;
- an `image` that carries no tag, since the tag is its own field;
- a `repo` that is http, https or git, with no `#ref`;
- `minCubeship` a valid version range.

Then advisories, which never block a publish: no health path means a
deploy cannot tell whether the app started; a floating tag or an
unpinned engine version means two installs of this template are two
different systems; no limits means one app can take the machine; an app
that nothing reaches, with no domain and no reference to it, is probably
a mistake.

A diagnostic is one object everywhere — in the editor, on publish, and
from the endpoint:

```ts
type Diagnostic = {
  severity: "error" | "warning" | "info";
  code: "reference.unknown";        // stable, linkable to the docs
  message: string;
  path: (string | number)[];        // ["apps", 0, "env", "DB_HOST"]
  range?: { start: Pos; end: Pos }; // absent when the key itself is missing
  hint?: string;
};
```

When a required key is missing there is no node to point at, so the range
anchors on the parent's key, and on the first line when even that is
gone.

Validation ends by emitting the normalized JSON: names filled in from
keys, environment defaulted to `production`, variables sorted, comments
dropped because the author's own text is stored separately. Beside the
editor, that JSON is rendered as what the template will create — every
app with its image and port, every database with its engine and version,
every attachment, and the internal hostnames spelled out under the
suggested project name.

## The registry

Postgres through Drizzle: one schema, generated SQL migrations committed,
applied by the container at start so there is no deploy step to forget.

| Table | Holds |
| --- | --- |
| `users` | one row per GitHub account: `github_id` unique, `login`, `name`, `avatar_url`, `role` of user or admin, `blocked_at` |
| `sessions` | `token_hash`, `user_id`, `expires_at`. The cookie carries 32 random bytes; the table stores only their SHA-256 |
| `templates` | `slug` unique and permanent, `author_id`, `name`, `summary`, `image_key`, `tags`, `status` of draft, published, unlisted or removed, `likes_count`, `current_version_id` |
| `template_versions` | immutable: `number`, `manifest` as normalized JSONB, `source` as the YAML the author typed, `schema_version`, `notes` |
| `likes` | `(template_id, user_id)` as the primary key, so a second like is a no-op rather than a second row |
| `comments` | `body`, `author_id`, `parent_id` for one level of replies, `deleted_at` so a removed comment keeps the thread's shape |
| `reports` | what was reported, by whom, why, and what was done |

**Editing publishes a new version.** Old ones stay readable and a
consumer can pin one. Likes and comments belong to the template, not the
version. Keeping both the normalized JSON and the author's YAML means the
editor reopens exactly what was written, comments and order included,
while consumers read something canonical.

**Moderation happens after publication.** Anyone can report, an admin can
unlist, take down or block. A pre-publish queue would make one person the
gate on every author's first impression, at a volume of zero.

Sign-in is GitHub and nothing else: `/api/auth/github` sets a short-lived
random state cookie and redirects; the callback rejects a mismatched
state, exchanges the code, reads the account once, upserts by
`github_id`, writes a session row and sets an httpOnly, secure,
SameSite-Lax cookie for 30 days. **The GitHub token is discarded after
that one read** — we need nothing further from GitHub, so keeping it
would be holding somebody's credential for no purpose. Admins come from
an environment variable of logins, so there is no escalation path through
the app. Abuse limits are counted from rows that already exist rather
than a table of their own.

There is no install counter and no `installs` table in this pass. Nothing
can write one until an instance can apply a template and report that it
did, and a table nobody writes is a column that lies on every card.

Out of scope, deliberately: account deletion and handing a template to
another author. Sign-out is all there is.

## The API

One surface. Everything is a route handler under `/api/v1`, and the
browser uses the endpoints an agent or an instance would. Server actions
for the UI plus an API for machines would be two validation paths for one
feature.

| Method and path | Who | What |
| --- | --- | --- |
| `GET /api/v1/templates` | anyone | the catalog, with `q`, `tag`, `sort` and a cursor |
| `GET /api/v1/templates/{slug}` | anyone | the record, its author, its counts, its current version |
| `GET /api/v1/templates/{slug}/versions` | anyone | the history |
| `GET /api/v1/templates/{slug}/manifest` | anyone | the normalized JSON alone, `?version=n` to pin. What an instance calls |
| `GET /schema/template/v1.json` | anyone | the generated schema, permanently |
| `POST /api/v1/validate` | anyone | YAML in, diagnostics out. Stateless |
| `POST /api/v1/templates` | author | create a draft |
| `PATCH /api/v1/templates/{slug}` | author | name, summary, tags, photo, status |
| `POST /api/v1/templates/{slug}/versions` | author | publish; refuses with diagnostics |
| `POST /api/v1/templates/{slug}/image` | author | one image, at most 2 MB, re-encoded to WebP |
| `PUT` and `DELETE /api/v1/templates/{slug}/like` | signed in | idempotent both ways |
| `GET` and `POST /api/v1/templates/{slug}/comments` | signed in to post | flat, one level of replies |
| `PATCH` and `DELETE /api/v1/comments/{id}` | author or admin | edit window, then soft delete |
| `POST /api/v1/reports` | signed in | report a template or a comment |
| `GET /api/v1/admin/reports` and friends | admin | the queue: unlist, take down, block |
| `GET /api/v1/me` | signed in | who the browser is |

A failure is always `{ error: { code, message } }`, with diagnostics
attached on 422. The prefix is `/api/v1` because an instance in the field
pins it. Public reads allow cross-origin GET without credentials, so a
dashboard or an agent reads the catalog directly. Photos are re-encoded
rather than stored as uploaded, and served through our own path from a
private bucket: an image accepted verbatim is an attack surface, and a
public bucket is a second way to reach it.

## The pages

In the site's existing vocabulary — `hud-frame` cards, the mono `label`
over each section, square corners, dark only, no new palette.

| Route | What is on it |
| --- | --- |
| `/templates` | the catalog: a square search field, tag filters, sort by newest or most liked, a grid of cards. A card is the photo, name, summary, author, like count, and what it creates |
| `/templates/{slug}` | the photo and name, what it creates, the file with a copy button, the inputs an installer must fill, the versions, the like button, the comments |
| `/templates/new` | the editor: name, summary, tags, photo, then the file with live diagnostics and the preview beside it |
| `/templates/{slug}/edit` | the same editor, publishing a new version, with notes |
| `/templates/{slug}/versions/{n}` | a past version, read only |
| `/u/{login}` | an author and their templates |
| `/me/templates` | your drafts and anything unlisted |
| `/admin/reports` | the queue |

The header gains a Templates link beside Docs and Changelog, the footer's
Product column gains one, and the landing page gains a section showing
four popular templates, because a catalog nobody sees gets no authors.

Three decisions. **The detail page advertises no command that does not
exist**: until an instance can apply a manifest, the page offers the file,
a copy button and the address of the JSON. **The signed-in state is
fetched by a small client component**, not read from the cookie in the
shared layout, which would make every page dynamic including the landing
page and the docs. **Comments are plain text**, so there is no sanitizer
to trust and no rendering surface to attack. The editor is the only heavy
page, so CodeMirror is imported dynamically and never reaches the
catalog.

## Running it

The site stops being stateless. Its configuration is deliberately the
variable names Cubeship itself injects, so attaching a managed Postgres
and a managed bucket is the entire setup, and cubeship.dev becomes the
product's own first user.

| Variable | Where it comes from |
| --- | --- |
| `DATABASE_URL` | attaching the managed Postgres at an empty prefix |
| `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `S3_PATH_STYLE` | attaching the managed bucket |
| `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | a GitHub OAuth app for cubeship.dev, set by hand |
| `ADMIN_LOGINS` | comma-separated GitHub logins |

There is no `SESSION_SECRET`. A session token is 32 random bytes and the
table stores only its SHA-256, so there is nothing to sign and nothing to
rotate; the OAuth state is compared against the copy in its own cookie
rather than signed against a server secret.

Three constraints that break the build if they are got wrong:

- **`next build` runs with no database**, in CI and in the image. Every
  page that queries Postgres renders at request time, the sitemap
  included, and no module reads an environment variable at import time.
- **Migrations run in `instrumentation.ts`, Next's server-start hook,
  not a container entrypoint script.** `output: "standalone"` traces
  only what code imports, so a loose script would need its own
  `node_modules`; the generated SQL files are named in
  `outputFileTracingIncludes` and copied into the image because of the
  same tracing. A Postgres advisory lock held for the migration keeps
  two containers starting at once from both applying the same file.
- **The site gains a test runner.** Vitest over the schema, the reference
  resolver and the diagnostics, in the `site` job. A bug in the validator
  publishes broken templates to everybody, which makes it the one part of
  this that has to be tested rather than clicked.

`make site-db-up` starts a Postgres for development, on its own port so
it cannot collide with the one the Go tests use. `make check` stays Go
only and keeps needing nothing.

## What is left for later

The daemon's half: a manifest applier, a Go validator that does not trust
the API's output, `cubeship template apply <slug>`, and a dashboard
browser over this catalog. It gets its own design, written against the
API above.

Two open risks, neither solved here. Apps have no volume and no command
override, which will be the first thing authors hit and is a daemon
change when it comes. And community content with image uploads and
comments is a spam surface, where reports plus takedown are a floor
rather than an answer.
