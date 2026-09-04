# Working on Cubeship

Self-hosted PaaS for one VPS. Go, module `cubeship`, no external
services beyond Docker. `README.md` is the operator's intro; this file is
for whoever (or whatever) edits the code.

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

| Package | Holds |
| --- | --- |
| `cmd/cubeshipd` | The daemon: config, bootstrap, HTTP server wiring |
| `cmd/cubeship` | The CLI (cobra), one file per noun |
| `internal/api` | HTTP handlers, authorization, the MCP server |
| `internal/store` | SQLite schema, migration, all queries |
| `internal/deploy` | The zero-downtime deploy orchestrator |
| `internal/dockerx` | Thin wrapper over the Docker Engine API |
| `internal/bootstrap` | Bringing up the registry and Traefik containers |
| `internal/regauth` | Registry v2 JWT token auth (per-user push/pull) |
| `internal/apiclient` | What the CLI talks to the daemon with |

## Store

Postgres through `pgx/v5/stdlib` over `database/sql`. Placeholders are
`$1`, `$2`, …, and there is **no `LastInsertId`** — an insert that needs
the new row returns it with `INSERT ... RETURNING <columns>`.

Schema changes are numbered, append-only migrations in
[`migrate.go`](internal/store/migrate.go): add an entry to `migrations`,
never edit one that has shipped. Postgres has transactional DDL, so each
one applies atomically alongside the row recording it, and `migrate` runs
on every daemon start.

Queries live as package-level functions over a `queryer` interface so
`*Store` and `*Tx` share them; `WithTx` is the transaction primitive.
Get* methods wrap `ErrNotFound`. Each table has a `<table>Columns`
constant that its scan function reads in order — change one, change both.

`env` columns are `JSONB`; write them through `marshalEnv` so a nil map
becomes `{}` rather than JSON null.

The daemon runs its own `cubeship-postgres` container (see
`bootstrap.PostgresContainerOpts`) unless `CUBESHIP_DATABASE_URL` points
it at an existing server.

## API and MCP

Handlers are grouped by resource (`apps_handlers.go`, `org_handlers.go`,
...). Authorization goes through `authorizeOrg`/`authorizeApp` and their
`*Request` wrappers; a resource the caller may not see returns **404, not
403**, so the API never confirms that another org's app exists.

`/mcp` serves the same capabilities as the CLI over the Model Context
Protocol, authenticated by the same bearer API key. It is **stateless on
purpose** — the server is rebuilt per request so its tools close over
that request's authenticated user, and no session can be reused across
users. Logic shared between an HTTP handler and an MCP tool goes in an
`*_actions.go` file so the two can never drift; a new CLI capability
should land as a handler, an action, and a tool.

## Tests

Unit tests need a real Postgres — there is no in-memory mode. `make test`
starts one (`make db-up`, a container on port 5433) and
[`storetest.New(t)`](internal/storetest/storetest.go) gives each test its
own schema inside it, dropped on cleanup, so tests stay isolated and can
run in parallel. Tests in package `store` itself can't import `storetest`
(import cycle) and use the equivalent local `newTestStore`.

A test with no database reachable **fails**, deliberately — skipping
would let `make check` report success for tests that never ran.

Docker is faked everywhere else, so no test talks to a real daemon. The
MCP tests run a real client against a real `httptest` server.
`test/integration` needs a Linux Docker daemon (`--network host` doesn't
reach the host on Docker Desktop) and sits behind `//go:build integration`.

Slugs — orgs, projects, environments, apps — are kebab-case and validated
against `slugPattern`, because they become path segments of a registry
image reference.
