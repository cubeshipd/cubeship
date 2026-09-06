-- +goose Up
-- +goose StatementBegin

-- A managed database belongs to the instance, not to a project.
--
-- 00018 put it inside an environment, so that an app inherited its
-- connection string through the layering it was already in. That is
-- true and it was not enough: on one VPS the common shape is a single
-- Postgres serving several small apps, and those apps are routinely in
-- different projects. A database owned by `web/production` could not be
-- reached from `blog/production` at all — not because anything
-- prevented it, but because the model had decided in advance that it
-- was the wrong thing to want.
--
-- So ownership moves to the attachment. A datastore exists on its own;
-- `datastore_attachments` is the whole of what connects it to anything,
-- and it may now cross projects and environments freely.
--
-- What is given up is that an environment no longer separates data by
-- itself: `pg-production` and `pg-staging` are two datastores, told
-- apart by their names, and attaching the wrong one to the wrong app is
-- now possible. That is a real cost, paid for a database that can be
-- shared — which is the reason to run one on a box this size.
ALTER TABLE datastores DROP CONSTRAINT datastores_project_id_fkey;
ALTER TABLE datastores DROP CONSTRAINT datastores_environment_id_fkey;
DROP INDEX datastores_environment_slug;
DROP INDEX datastores_project;

-- The slug was unique within an environment and is now unique on the
-- instance, so two that only differed by environment would collide.
-- Renaming the later one beats aborting the migration and leaving a
-- daemon that will not start: the name is what has to change, and this
-- says which one changed.
UPDATE datastores d SET slug = d.slug || '-' || d.id
WHERE EXISTS (
    SELECT 1 FROM datastores other
    WHERE other.slug = d.slug AND other.id < d.id
);

ALTER TABLE datastores DROP COLUMN project_id;
ALTER TABLE datastores DROP COLUMN environment_id;

CREATE UNIQUE INDEX datastores_slug ON datastores (slug);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Irreversible in the way that matters: which project and environment a
-- datastore belonged to is not recorded anywhere once the columns are
-- gone, and there is no answer to invent — an attachment may now name
-- apps in several. The columns come back so the schema matches, holding
-- the first project and environment the datastore is attached into, and
-- 0 for one attached to nothing.
ALTER TABLE datastores ADD COLUMN project_id BIGINT;
ALTER TABLE datastores ADD COLUMN environment_id BIGINT;

UPDATE datastores d SET
    project_id = COALESCE((
        SELECT a.project_id FROM datastore_attachments t
        JOIN apps a ON a.id = t.app_id
        WHERE t.datastore_id = d.id ORDER BY t.id LIMIT 1), 0),
    environment_id = COALESCE((
        SELECT a.environment_id FROM datastore_attachments t
        JOIN apps a ON a.id = t.app_id
        WHERE t.datastore_id = d.id ORDER BY t.id LIMIT 1), 0);

DROP INDEX datastores_slug;

-- +goose StatementEnd
