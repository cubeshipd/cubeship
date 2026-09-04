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

SQLite through `modernc.org/sqlite` — pure Go, no cgo, and **no
`PRAGMA foreign_keys`**: the `REFERENCES` clauses in the schema document
intent, they are not enforced.

`store.Open` runs the schema and then migrates idempotently: `hasColumn`,
then `ALTER TABLE ... ADD COLUMN ... NOT NULL DEFAULT <literal>`, then
backfill. Two SQLite rules bite here — a DDL default must be a literal
constant (bound `?` parameters are rejected), and string literals take
single quotes (double quotes mean an identifier). Every migration must
survive running twice.

Queries live as package-level functions over a `queryer` interface so
`*Store` and `*Tx` share them; `WithTx` is the transaction primitive.
Get* methods wrap `ErrNotFound`.

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

Unit tests use a real SQLite database in `t.TempDir()` and a fake Docker
client — no daemon, no network. The MCP tests run a real client against a
real `httptest` server. `test/integration` needs a Linux Docker daemon
(`--network host` doesn't reach the host on Docker Desktop) and sits
behind `//go:build integration`.

Slugs — orgs, projects, environments, apps — are kebab-case and validated
against `slugPattern`, because they become path segments of a registry
image reference.
