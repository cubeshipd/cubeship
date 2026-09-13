-- +goose Up
-- +goose StatementBegin

-- A managed database lives in an environment, beside the apps that use
-- it — not in a list of its own on the instance.
--
-- The reason is the same one that removed organizations: a flat list
-- would have made people spell the environment into the name.
-- `api-staging-pg` is the prefix this table already has, typed by hand
-- and unenforced. Staging and production must hold different data, and
-- an environment is exactly the thing that says so.
--
-- Everything follows from that. Deleting a project or an environment
-- takes its databases the way it takes its apps, and an app reaches one
-- through the environment layering it already inherits.
CREATE TABLE datastores (
    id             BIGSERIAL PRIMARY KEY,
    project_id     BIGINT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    environment_id BIGINT NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    -- Unique within its environment, like an app's name, and permanent
    -- for the same reason: it is a component of the container name its
    -- apps resolve on the shared network, so renaming it would silently
    -- break every connection string already handed out.
    slug           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    -- Which server this runs, and which tag of it. Neither is editable:
    -- an engine change is a different database, and a major version
    -- changed under a data directory is how you corrupt one.
    engine         TEXT NOT NULL,
    version        TEXT NOT NULL,
    username       TEXT NOT NULL,
    -- Stored as given, like an external registry's login and for the
    -- same reason: a hash cannot connect to anything. It is never
    -- returned by the endpoint that lists these — reading it is its own
    -- request, and an admin's.
    password       TEXT NOT NULL,
    -- The database created inside the server on first start. Empty for
    -- an engine that has no such concept.
    database_name  TEXT NOT NULL DEFAULT '',
    -- The host port this answers on from outside the instance, or 0 for
    -- "only its neighbours can reach it", which is the default.
    exposed_port   INTEGER NOT NULL DEFAULT 0,
    container_id   TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'provisioning',
    -- Why provisioning failed, when it did. There is no deployments
    -- table here: a database is provisioned once and then runs, so the
    -- outcome belongs on the row rather than in a history of attempts.
    error          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX datastores_environment_slug ON datastores (environment_id, slug);
CREATE INDEX datastores_project ON datastores (project_id);

-- One port, one listener. Two rows claiming one would be a container
-- that cannot start, discovered minutes later by whoever was watching.
CREATE UNIQUE INDEX datastores_exposed_port ON datastores (exposed_port)
    WHERE exposed_port <> 0;

-- Which apps may reach which database, and under what variable names.
--
-- Explicit rather than "every app in the environment gets it": two apps
-- in one environment routinely want different databases, and an
-- attachment is also what makes deleting a database able to say what
-- breaks.
CREATE TABLE datastore_attachments (
    id           BIGSERIAL PRIMARY KEY,
    datastore_id BIGINT NOT NULL REFERENCES datastores (id) ON DELETE CASCADE,
    app_id       BIGINT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    -- What the injected variables are named under. Empty is the usual
    -- answer and gives DATABASE_URL; a second database on one app needs
    -- a prefix, or the two would be one variable with two values.
    prefix       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX datastore_attachments_app_prefix ON datastore_attachments (app_id, prefix);
CREATE UNIQUE INDEX datastore_attachments_pair ON datastore_attachments (datastore_id, app_id);
CREATE INDEX datastore_attachments_datastore ON datastore_attachments (datastore_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE datastore_attachments;
DROP TABLE datastores;
-- +goose StatementEnd
