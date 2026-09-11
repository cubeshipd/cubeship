# Working on Cubeship

Everything a person installing Cubeship needs is in the
[README](README.md). This is for changing it.

## Contents

- [What you need](#what-you-need)
- [Running it locally](#running-it-locally)
- [Before you commit](#before-you-commit)
- [Tests](#tests)
- [How the code is laid out](#how-the-code-is-laid-out)
- [Commits and pull requests](#commits-and-pull-requests)
- [Cutting a release](#cutting-a-release)

## What you need

- **Go** (the version in [go.mod](go.mod)) and **pnpm** for the dashboard.
- **Docker**, for the Postgres the tests run against — and for anything
  that starts a container, which is most of what this daemon does.

Nothing else, and nothing global: the toolchain is what the Makefile
calls.

```bash
git clone https://github.com/cubeshipd/cubeship && cd cubeship
make build
```

## Running it locally

Two processes, because the dashboard has its own dev server:

```bash
make dev        # the daemon on this machine, rebuilding on every Go change
make web-dev    # the dashboard on :3001, hot reloading
```

The daemon proxies page requests to whichever of those is there, so
`http://localhost:3000` is the whole instance either way — one address,
the same as a real install.

`make dev` runs the daemon as a **host process** rather than a container,
which is a mode the code knows about: `config.InContainer` decides every
address, and the four places that branch on it are pinned by a test.
Containers it starts reach back through `host.docker.internal`.

Its state goes in `~/.cubeship-dev`, and its Postgres is the same
container the tests use.

## Before you commit

```bash
make check
```

gofmt, `go vet` — including the build-tagged integration test, which a
plain `go vet ./...` never compiles — the generated changelog, shell
syntax, and the unit tests under `-race`.

**It needs no Docker, no Postgres and no network.** Standing those up to
find out whether a function returns the right string is time taken out
of the edit-run loop, and CI is where nobody is waiting.

Judge it by its exit code. It stops at the first failing step, so a
green-looking tail is not a green run.

## Tests

| | |
| --- | --- |
| `make check` | what must pass before a commit — nothing to start |
| `make test-db` | the same tests with the Postgres they want |
| `make test-integration` | a real daemon, registry and Traefik. Linux only |
| `make test-install` | `install.sh` end to end, on a real Debian |
| CI | all of it, including the two a Mac cannot run |

Unit tests need a real Postgres; there is no in-memory mode. `make test`
starts one in a container on port 5433 and gives each test its own
schema, so tests stay isolated and run in parallel against one server.

**A test with no database reachable fails rather than skipping**, except
under `-short`, which is what a laptop passes. Skipping would let a green
run mean tests that never ran.

The suite is deliberately not exhaustive. It covers what is expensive to
get wrong: the authorization matrix, deploy ordering and rollback,
transaction rollback, registry scope grants, and that the MCP surface
says the same thing the HTTP one does. Docker is always faked.

## How the code is laid out

By domain, not by technical layer. A module owns everything about one
concept — its entity, its persistence, its use cases, and every surface
it is reached through:

| File | Holds |
| --- | --- |
| `<name>.go` | the entity, its constants and its domain errors |
| `repository.go` | every SQL statement for its tables |
| `service.go` | the use cases — the only place business rules live |
| `http.go` | handlers, routes, and the domain-error → status mapping |
| `mcp.go` | the MCP tools |
| `openapi.go` | the OpenAPI operations for the routes in `http.go` |

`http.go` and `mcp.go` are adapters and nothing else. They parse input,
call one service method, and render the result — a rule that lives in a
handler is a rule the MCP surface does not have, which is exactly how
the two drifted apart before this layout.

**[AGENTS.md](AGENTS.md) is the conventions every change follows**, and
it is worth reading before a first one. The long version is
[docs/design/](docs/design), one file per area: every decision in this
codebase is written down there with the reason it was made, including
the ones that were made twice. AGENTS.md indexes them, and says which to
open for what you are about to touch.

## Commits and pull requests

- **Say what changes**, in the imperative, in the subject. `Cap what an
  app may take from its machine`, not `feat(app): add limits`. The
  subject is what somebody reads in a changelog a year later.
- A body only when the *why* is not obvious from the diff. Six lines is
  usually plenty.
- **No conventional-commit prefixes**, and no generated changelog: the
  release notes are written by hand, because they end up in a dialog
  people read.
- **Never credit an AI agent** — not in a commit, a pull request, or a
  code comment.
- Work happens on `master`, in the repository root.

## Cutting a release

Write the notes first. They are the release, three ways: the body of the
GitHub release, `CHANGELOG.md`, and the dialog the dashboard shows after
an upgrade.

```bash
$EDITOR internal/release/notes/0.2.0.md   # version, date, summary, then the notes
make changelog                            # regenerate CHANGELOG.md
git commit -am "Release notes for 0.2.0"
make release VERSION=0.2.0
```

`make release` refuses a version with no notes and a stale changelog,
then tags and pushes. The workflow builds both images for both
architectures — each on a machine of that architecture — attaches a
provenance attestation, and publishes the release.

A version with a hyphen in it is a **prerelease**: `0.2.0-rc.1`
publishes its own tag and does not move `latest`, so nobody who
installed without naming a version is upgraded onto a candidate.
