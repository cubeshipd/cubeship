-- +goose Up
-- Backing up the instance itself.
--
-- `internal/backup` was built above `datastore` rather than inside it
-- for exactly this: **the most valuable database on this box is
-- Cubeship's own Postgres**, which holds every account, project, app,
-- credential and attachment, and is not a datastore at all. A module
-- that could never reach it would have been the wrong module.
--
-- `kind` is what tells the two apart, and it is a column rather than
-- "datastore_id IS NULL" because that already means something else:
-- the foreign key is ON DELETE SET NULL, so a null there is a dump
-- whose database was deleted — the orphans, which are kept on purpose
-- and shown on their own. An instance backup has never had a datastore
-- and never will.
ALTER TABLE backups ADD COLUMN kind TEXT NOT NULL DEFAULT 'datastore';

-- The instance's own schedule.
--
-- **A table of its own, because there is one instance.**
-- `backup_schedules` is keyed by the datastore it belongs to, which an
-- instance backup has none of — and widening that key to nullable would
-- make "the row with no database" a shape the unique index cannot
-- express. One row, enforced by the primary key being a constant.
CREATE TABLE instance_backup_schedule (
    -- Always true, so a second row cannot exist. The alternative is a
    -- unique index on a constant expression, which says the same thing
    -- less plainly.
    only            BOOLEAN PRIMARY KEY DEFAULT true CHECK (only),
    at              TEXT NOT NULL,
    timezone        TEXT NOT NULL,
    keep            INT NOT NULL DEFAULT 7,
    object_store_id BIGINT REFERENCES object_stores(id) ON DELETE SET NULL,
    bucket          TEXT NOT NULL DEFAULT '',
    last_run_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE instance_backup_schedule;
ALTER TABLE backups DROP COLUMN kind;
