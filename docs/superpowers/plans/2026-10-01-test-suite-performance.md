# Test Suite Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce pull request feedback time by keeping fast tests fast and retaining a smaller, explicit database suite for persistence and integration invariants.

**Architecture:** Keep real Postgres for tests that prove SQL, migrations, transactions, authorization persistence, and critical HTTP flows. Move pure validation, domain calculations, probe selection, rendering, and adapter behavior into dependency-free tests. The fast CI job runs the full `-short` suite; the database job runs only the packages and tests that require Postgres, avoiding a second full package run.

**Tech Stack:** Go testing, `testing.Short`, PostgreSQL test schemas, GitHub Actions, race detector.

**Spec:** CUB-12 review and CI performance requirements in the issue thread.

## Global Constraints

- Preserve real Postgres coverage for SQL, migrations, transaction rollback, and authorization persistence.
- Do not replace database tests with mocks when the behavior under test is repository or transaction behavior.
- Keep `-race` in CI; provide a faster non-race local command only if measurement shows it helps feedback.
- Keep Docker/integration and installer jobs separate from unit and database jobs.
- Tests with no database must remain runnable without Docker, Postgres, or network access.

## Review Focus

- A test moved out of the DB suite must still prove the same behavior without weakening a trust boundary: retain an HTTP or repository test where persistence or authorization matters.
- Parallel DB tests must not share mutable schema state: retain per-test isolation or clone an already-migrated template.
- The fast job must not silently skip a test because it lacks infrastructure: `-short` skips only tests that explicitly require the database.
- Deploy tests must not regain real timing waits: keep fake Docker and zero intervals for unit-level orchestration tests.
- CI status must show a failed database job separately from a failed fast job so the first failure is actionable.

### Task 1: Measure and classify the current suite

**Files:**
- Create: `docs/testing/test-suite-map.md`
- Inspect: `product/internal/platform/database/dbtest/dbtest.go`, `product/internal/server/servertest/servertest.go`, package test files

- [ ] Run package-level timing with `go test -short -json` and full `go test -json` against the existing Postgres container.
- [ ] Record which packages call `dbtest.New`, `servertest.New`, or `RequireDatabase`, and which tests are pure.
- [ ] Identify the ten slowest tests and classify each as repository, service, HTTP, orchestration, or pure logic.
- [ ] Save the map and a baseline command/output summary.

### Task 2: Extract high-value pure tests

**Files:**
- Modify: the slowest mixed test files identified in Task 1
- Create: focused pure test files beside domain helpers where needed

- [ ] Move validation, parsing, merge/precedence, port selection, probe dispatch, route rendering, and status mapping assertions to tests that do not construct `dbtest` or `servertest` fixtures.
- [ ] Keep one representative HTTP test for each public behavior that depends on routing or authorization.
- [ ] Run the affected package with `go test -short -race` and confirm it no longer opens Postgres for the moved cases.

### Task 3: Reduce redundant database flows

**Files:**
- Modify: duplicated `*_http_test.go`, service, and orchestration test files identified in Task 1
- Inspect: each package's repository tests and authorization tests

- [ ] Retain repository tests for SQL reads/writes, constraints, migrations, transaction rollback, and JSONB behavior.
- [ ] Retain one end-to-end HTTP test per critical authorization/deploy/rollback path; remove repeated success-path permutations that assert the same persistence behavior.
- [ ] Preserve failure cases that protect data loss, privilege escalation, deploy ordering, and rollback.
- [ ] Run the affected packages with the database and compare assertions against the test-suite map.

### Task 4: Make database execution explicit in CI

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `Makefile`

- [ ] Keep `Fast checks` on `-short -race` and repository-only checks.
- [ ] Add a database test command that runs only the classified DB packages, rather than `go test ./...` a second time.
- [ ] Keep Postgres service setup only on the database job.
- [ ] Run fast and database jobs in parallel and report their durations separately.

### Task 5: Optimize isolated Postgres setup only after measurement

**Files:**
- Modify: `product/internal/platform/database/dbtest/dbtest.go`
- Test: `product/internal/platform/database/dbtest`

- [ ] Measure schema creation, migration, and cleanup time separately.
- [ ] If migrations dominate, create one migrated template per test process and clone its schema for each test while preserving isolation.
- [ ] Keep the current per-test migration path as a fallback for unsupported PostgreSQL features.
- [ ] Verify parallel tests, rollback tests, and cleanup behavior before adopting the optimization.

### Task 6: Verify and document the new gates

**Files:**
- Modify: `docs/testing/test-suite-map.md`
- Modify: `.github/workflows/ci.yml`

- [ ] Run the fast command locally without Postgres.
- [ ] Run the database command against the Docker Postgres service.
- [ ] Run integration and installer jobs or their repository equivalents.
- [ ] Compare wall-clock timings against Task 1 and stop if the change does not materially improve feedback time.

