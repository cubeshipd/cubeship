-- +goose Up
-- +goose StatementBegin

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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE api_keys;
DROP TABLE memberships;
DROP TABLE users;
DROP TABLE deployments;
DROP TABLE apps;
DROP TABLE environments;
DROP TABLE projects;
DROP TABLE organizations;

-- +goose StatementEnd
