package store

import (
	"context"
	"database/sql"
	"fmt"
)

// A migration is one numbered, irreversible step from the previous
// schema version to this one. Never edit a migration that has shipped —
// a database that already applied it will not run it again. Add a new
// one instead.
type migration struct {
	version int
	name    string
	sql     string
}

// migrations must stay ordered by version, with no gaps.
var migrations = []migration{
	{1, "initial schema", initialSchema},
}

// migrate applies every migration the database hasn't seen yet, each in
// its own transaction alongside the row recording it. Postgres has
// transactional DDL, so a migration that fails halfway leaves no trace
// and no half-applied schema — the whole reason this is simpler than the
// SQLite version it replaces.
func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	// COALESCE, not a NULL scan: MAX over an empty table is one NULL row,
	// not zero rows, so this would otherwise fail to scan into an int on
	// a brand-new database.
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", m.version, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.version, m.name); err != nil {
		return fmt.Errorf("record migration %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", m.version, err)
	}
	return nil
}

// initialSchema is the whole schema as of the move from SQLite to
// Postgres. There is no upgrade path from a SQLite database — that
// release is superseded, and the daemon starts against an empty Postgres.
//
// env columns are JSONB rather than TEXT so a malformed value can't be
// stored at all, and so future queries can filter on individual keys.
const initialSchema = `
CREATE TABLE organizations (
	id BIGSERIAL PRIMARY KEY,
	slug TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE projects (
	id BIGSERIAL PRIMARY KEY,
	org_id BIGINT NOT NULL REFERENCES organizations(id),
	slug TEXT NOT NULL,
	name TEXT NOT NULL,
	env JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (org_id, slug)
);

CREATE TABLE environments (
	id BIGSERIAL PRIMARY KEY,
	project_id BIGINT NOT NULL REFERENCES projects(id),
	slug TEXT NOT NULL,
	name TEXT NOT NULL,
	env JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (project_id, slug)
);

-- TODO: name and image are unique across the whole instance, so one app
-- name can exist only once per Cubeship — the same app cannot live in
-- both "production" and "staging", and two organizations cannot both
-- have an "api". Scoping them to environment_id is an API-level redesign
-- (routes, registry image paths, Traefik router names) and gets its own
-- migration.
CREATE TABLE apps (
	id BIGSERIAL PRIMARY KEY,
	org_id BIGINT NOT NULL REFERENCES organizations(id),
	project_id BIGINT NOT NULL REFERENCES projects(id),
	environment_id BIGINT NOT NULL REFERENCES environments(id),
	name TEXT NOT NULL UNIQUE,
	domain TEXT NOT NULL,
	image TEXT NOT NULL UNIQUE,
	container_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	env JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE deployments (
	id BIGSERIAL PRIMARY KEY,
	app_id BIGINT NOT NULL REFERENCES apps(id),
	image_ref TEXT NOT NULL,
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
	id BIGSERIAL PRIMARY KEY,
	username TEXT NOT NULL UNIQUE,
	is_super_admin BOOLEAN NOT NULL DEFAULT false,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
	user_id BIGINT NOT NULL REFERENCES users(id),
	org_id BIGINT NOT NULL REFERENCES organizations(id),
	role TEXT NOT NULL,
	PRIMARY KEY (user_id, org_id)
);

CREATE TABLE api_keys (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id),
	key_hash TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	last_used_at TIMESTAMPTZ
);

-- Every column a hot query filters or joins on that isn't already
-- covered by a primary key or unique constraint. Without these, listing
-- apps or resolving a membership is a sequential scan.
CREATE INDEX projects_org_id_idx ON projects (org_id);
CREATE INDEX environments_project_id_idx ON environments (project_id);
CREATE INDEX apps_org_id_idx ON apps (org_id);
CREATE INDEX apps_project_id_idx ON apps (project_id);
CREATE INDEX apps_environment_id_idx ON apps (environment_id);
CREATE INDEX deployments_app_id_idx ON deployments (app_id);
CREATE INDEX memberships_org_id_idx ON memberships (org_id);
CREATE INDEX api_keys_user_id_idx ON api_keys (user_id);
`
