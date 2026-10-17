---
name: core-database
description: Change Core PostgreSQL repositories, migrations, or DB-backed tests without breaking transactional and schema contracts.
---

# Core database

- Repositories use `database.Queryer` so the same SQL runs on the pool or inside `DB.WithTx`. Keep queries in the owning domain repository, with `$1` placeholders and `INSERT ... RETURNING` for generated values.
- Migrations live in `core/internal/platform/database/migrations`. Add numbered goose Up/Down files; never edit a shipped migration.
- If a table's scanned columns change, update its shared column list, scan function, and joins that qualify the list. Preserve JSONB empty-map behavior through `envvar.MarshalJSONB`.
- DB-backed tests use `dbtest.New(t)` or `dbtest.RequireDatabase(t)` and share one Postgres through isolated schemas. `-short` skips those tests; without `-short`, an unavailable database must fail. Keep Docker faked in unit tests.
- Prove repository or migration changes with focused DB-backed tests and run `make check` before committing. `make check` alone does not exercise DB-backed tests.
