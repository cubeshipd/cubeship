-- +goose Up
-- Backups of the databases this instance runs.

-- A backup outlives the database it came from, which is the whole
-- reason the columns below are written down rather than joined.
--
-- Deleting a datastore is exactly the moment its backups matter, so the
-- foreign key lets go instead of cascading: the rows stay, the files
-- stay, and the engine and version stay with them — a dump of Postgres
-- 17 has to say so, because a data directory written by one major
-- version is not readable by another and neither is a dump loaded into
-- the wrong one.
CREATE TABLE backups (
    id             BIGSERIAL PRIMARY KEY,
    datastore_id   BIGINT REFERENCES datastores(id) ON DELETE SET NULL,
    datastore_name TEXT NOT NULL,
    engine         TEXT NOT NULL,
    version        TEXT NOT NULL,

    -- Where it landed. A null store is the local disk, which is not a
    -- backup and every screen showing one says so.
    object_store_id BIGINT REFERENCES object_stores(id) ON DELETE SET NULL,
    bucket          TEXT NOT NULL DEFAULT '',
    object_key      TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL DEFAULT 0,

    -- running, succeeded, failed. A row is written before the dump
    -- starts, for the reason a deployment row is: the work is detached,
    -- nobody is on the connection, and where the outcome goes has to
    -- exist before there is one.
    status    TEXT NOT NULL DEFAULT 'running',
    error     TEXT NOT NULL DEFAULT '',
    scheduled BOOLEAN NOT NULL DEFAULT FALSE,

    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX backups_by_datastore ON backups (datastore_id, started_at DESC);

-- One row per database, and **the row existing is what scheduled
-- means**. No separate flag, so nothing can say a schedule is off while
-- a time sits beside it.
--
-- It cascades where the backups do not: a schedule for a database that
-- is gone is a timer with nothing to run.
CREATE TABLE backup_schedules (
    datastore_id    BIGINT PRIMARY KEY REFERENCES datastores(id) ON DELETE CASCADE,
    at              TEXT NOT NULL,
    timezone        TEXT NOT NULL,
    -- How many to keep. Zero keeps every one, which is a decision
    -- somebody can make and the screen warns about rather than a gap.
    keep            INT NOT NULL DEFAULT 7,
    object_store_id BIGINT REFERENCES object_stores(id) ON DELETE SET NULL,
    bucket          TEXT NOT NULL DEFAULT '',
    last_run_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE backup_schedules;
DROP TABLE backups;
