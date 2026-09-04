# Organizations, Users, and Registry Token Auth — Design

## Context

Cubeship's core (sub-project 1) is a single-tenant deploy engine: one
shared API bearer token, one shared registry credential, apps identified
by a globally-unique name. The user runs multiple companies' projects on
one VPS and wants other people to have panel/CLI access — this requires
real multi-tenancy: organizations that isolate apps and registry
namespaces from each other, and users (not just "the operator") with
per-user credentials and per-org roles.

This was originally deferred in sub-project 1's spec ("Multi-tenant /
multi-user auth ... future, post-personal-use validation"). It's being
pulled forward now, pre-empting sub-projects "web UI" and "managed
databases" from the original roadmap, since ownership boundaries
(who can see/touch what) are foundational to those too.

## Goals

- Multiple organizations on one VPS, each with its own apps and registry
  namespace; an org's apps/images are invisible to and unreachable by
  another org's credentials.
- Users, each with their own API key, no more shared bearer token.
- A user can belong to multiple organizations, with a role (`admin` or
  `member`) per organization.
- One super-admin level above organizations: can create orgs and each
  org's first admin, and has implicit access everywhere. This is the VPS
  operator.
- `docker push`/`pull` against the registry is authorized per-org via a
  real token-issuing flow (Docker Registry v2 token authentication),
  not just per-daemon-instance like today's htpasswd.
- CLI-only for now — no web panel in this sub-project.

## Non-goals

- Web UI (separate sub-project; this design's API is what it will build on).
- Billing, usage metering, or any other SaaS concern.
- Self-service signup or email/link invites — only admins/super-admin
  create users.
- Cross-org resource sharing (an app or image visible to two orgs at once).
- Infrastructure isolation per org: Traefik and the Docker network stay
  shared. Only app ownership and the registry namespace are org-scoped.
- Multiple named API keys per user, key expiry/scheduling — one active
  key per user; rotation is revoke-and-reissue.
- Changing how custom domains work — `app create --domain` already takes
  an arbitrary domain per app today (not derived from any org setting),
  so different companies' own domains already work with no change here.

## Data model

New tables in the existing SQLite store (`internal/store`), alongside
the existing `apps`/`deployments`:

```sql
CREATE TABLE organizations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slug TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	is_super_admin INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE memberships (
	user_id INTEGER NOT NULL REFERENCES users(id),
	org_id INTEGER NOT NULL REFERENCES organizations(id),
	role TEXT NOT NULL CHECK (role IN ('admin', 'member')),
	PRIMARY KEY (user_id, org_id)
);

CREATE TABLE api_keys (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	key_hash TEXT NOT NULL UNIQUE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_used_at DATETIME
);
```

`apps` gains `org_id INTEGER NOT NULL REFERENCES organizations(id)`, and
its uniqueness constraint changes from `name UNIQUE` to
`UNIQUE(org_id, name)` — an app name only has to be unique within its
org. The registry repository path becomes
`<org-slug>/<app-name>` (was just `<app-name>`), so the daemon's stored
`Image` field becomes `registry.<domain>/<org-slug>/<app-name>`.

API keys are never stored in plaintext — `key_hash` is a SHA-256 hash of
the key; the raw key is shown to the caller exactly once, at creation
time, the same way GitHub/GitLab personal access tokens work.

## Auth model — daemon API

`Authorization: Bearer <api-key>` now resolves to a user (via
`key_hash` lookup), not a fixed string compare. Every endpoint that acts
on an app resolves that app's `org_id` and checks: is the caller
super-admin, or does the caller have a membership row for that org (any
role, for read/deploy-type actions; `admin` role specifically for
destructive/membership actions)? `app create` requires the caller to be
at least `member` of the target org (org is specified explicitly — see
CLI below). Org/user management endpoints (`org create`, `user create`,
adding a membership) require super-admin (for creating orgs and an org's
first admin) or `admin` role within that org (for adding further
members to an org they already administer).

## Registry isolation — Docker Registry v2 token authentication

The registry's `config.yml` (written by `bootstrap.WriteRegistryConfig`,
already the mechanism sub-project 1 uses) switches from `auth: htpasswd`
to:

```yaml
auth:
  token:
    realm: https://api.<domain>/v2/token
    service: cubeship-registry
    issuer: cubeship
    rootcertbundle: /etc/docker/registry/token.crt
```

The daemon generates an RSA keypair at install time (persisted under
`DataDir`, alongside the existing token file); the public cert is
written to the path `rootcertbundle` points at and bind-mounted into the
registry container, mirroring how `WriteRegistryHtpasswd` already mounts
a generated file today. The private key never leaves the daemon.

Flow: `docker login`/`push`/`pull` without a valid bearer gets a `401`
from the registry with `WWW-Authenticate: Bearer realm=...,
service=...,scope="repository:<org-slug>/<app>:pull,push"`. The Docker
client then requests a token from `realm`, sending the requested
`service`/`scope` as query params and the user's credentials
(username + API key) as HTTP Basic auth. The daemon's new `GET
/v2/token` handler: validates the API key exactly like the main API
does, parses the requested repository from `scope`, checks the caller
has a membership in the org matching the repository's `<org-slug>`
prefix, and — only for the actions actually authorized — returns a
signed JWT (`RS256`, using the daemon's private key) with an `access`
claim scoped to exactly what was granted. An unauthorized scope is
simply omitted from the returned token's `access` array (per the token
spec), which the registry then treats as denied for that action.

This means: `docker login registry.<domain> -u <username> -p <api-key>`
authenticates as that user; a subsequent `docker push
registry.<domain>/other-org-slug/app` is issued a token with no access
to that repository, and the registry rejects the push. Same account,
same key, different org namespace — denied.

## Bootstrap

On first boot, if no rows exist in `users`, the daemon creates a
super-admin user and one API key for them — this replaces (not just
extends) today's single-`CUBESHIP_TOKEN`-is-the-API-token model. If
`CUBESHIP_TOKEN` is set, its value seeds the super-admin's key (keeping
today's "operator can pin a known value" convenience); otherwise one is
generated and persisted under `DataDir`, printed once on first boot the
same way the existing token is logged today (as a fingerprint, per the
sub-project 1 fix wave — the raw key itself goes to the persisted file,
not the log). The super-admin then creates organizations and each org's
first admin via the CLI/API; that admin can add further members to
their own org.

## CLI changes

New commands: `cubeship org create <name>` (super-admin), `cubeship org
list` (any authenticated user — filtered to orgs they're a member of,
or all orgs for super-admin), `cubeship user create <username> --org
<slug> --role <admin|member>` (super-admin, or an org admin adding to
their own org), `cubeship user api-key rotate` (any user, self-service —
revokes the caller's current key and issues a new one).

`cubeship app create` gains a required `--org <slug>` flag. Every other
app command (`deploy`, `logs`, `env set`) stays unchanged — the daemon
already knows which org owns an app by name lookup, so there's no need
for a CLI-side "current org" context to switch between.

`cubeship login` and `cubeship registry login` are unchanged in shape
(daemon URL + credential, and a `docker login` wrapper respectively) —
only the credential itself now identifies a specific user rather than
being the one shared secret.

## Error handling

- An API key that doesn't match any `key_hash`: `401`, same shape as
  today's "wrong token" response.
- A valid user acting on an org they have no membership in: `403`, not
  `404` — existence of an org/app isn't hidden from other orgs' users
  at this layer (the app names themselves already leak nothing since
  they're now scoped by org in the URL/registry path).
- `app create` naming a nonexistent `--org`: `404`.
- Registry token requests for a scope the caller isn't authorized for:
  per the Docker token spec, the token is still issued (HTTP 200) but
  with that access omitted — this is a protocol requirement, not a
  bug, and the registry itself then returns the actual `401`/`403` to
  the Docker client on the push/pull attempt.
- Deleting the last admin of an org, or a user's own membership, so an
  org ends up with zero admins: rejected with a clear error — an org
  must always have at least one admin (super-admin can still act on it
  regardless, but a leaderless org would be a real operational trap for
  its own members).

## Testing

- Unit tests for the store's new tables and queries (membership lookup,
  role checks, app-name uniqueness scoped per org) — same pattern as
  the existing `internal/store` tests, real SQLite, no mocks.
- Unit tests for the API authorization layer: a member can deploy their
  org's app, cannot touch another org's app (403), an org admin can add
  a member, a member cannot, super-admin can do everything.
- Unit tests for the registry token handler: valid credentials + scope
  the user's org owns → token with that access; valid credentials +
  scope outside their orgs → token with that access omitted; invalid
  credentials → the handler's own 401, before any token is issued.
- An integration-style test verifying an actual signed JWT (using the
  real signing code, not a fake) can be verified against the daemon's
  own public cert output — protects against a subtly wrong claims
  shape that unit tests against a mocked signer wouldn't catch.
- The existing sub-project 1 Docker-based integration test needs
  updating for the new `<org-slug>/<app>` registry path and the new
  login flow — flagged here, detailed in the implementation plan.

## Decisions carried from brainstorming

- Users can belong to multiple organizations (membership table with a
  role per org), not one org per user.
- Two roles within an org: `admin`, `member`. No per-app granular
  permissions.
- A super-admin level above organizations exists (the VPS operator);
  resolves bootstrap cleanly (first user created is the super-admin).
- Registry isolation via Docker Registry v2 token authentication (the
  daemon becomes a token-issuing server), not one registry container per
  org and not a shared credential with no per-org enforcement.
- Admin-created users only — no self-signup, no email/invite flow, since
  there's no web UI yet to make that a good experience anyway.
