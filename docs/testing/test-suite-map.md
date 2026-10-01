# Test suite map

Baseline measured on 2026-10-01 with the local `cubeship-test-db` container.

## Current gates

- `make check` runs `go test -short -race`; database tests explicitly skip under `-short`.
- CI's Database tests run the full product and catalog suites with `-race`.
- The largest package in the short run was `internal/app` at 6.1 seconds before the log test optimization.
- The largest package in the full CI run was `internal/app` at about 211 seconds, including real database-backed HTTP and deploy flows.

## What belongs in each layer

| Layer | Keep here | Infrastructure |
| --- | --- | --- |
| Pure unit | parsing, validation, merges, port selection, probe dispatch, route rendering, status mapping | none |
| Repository | SQL shape, constraints, JSONB, migrations, rollback | Postgres |
| Service | authorization and business invariants that depend on persistence | Postgres where repositories are involved |
| HTTP integration | representative route/auth/deploy flows and response contracts | Postgres |
| Integration | Docker, Traefik, installer and real process behavior | Docker/Linux |

## First optimization

`TestOutputIsCappedKeepingTheEnd` wrote the same noisy line 40,000 times through
the writer. It now writes one equivalent block, preserving the truncation and
tail assertions while removing per-call test overhead.

## Next measurement target

The full `internal/app` package is the main database-suite cost center. Its
HTTP tests use `servertest.New`, so the next reduction should remove duplicate
success-path fixtures while retaining repository, authorization, deploy order,
rollback, and probe-selection coverage.
