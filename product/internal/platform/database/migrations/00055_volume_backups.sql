-- +goose Up
-- A volume's backup keeps its row when the volume goes, like a database's.
-- The app reference is in datastore_name and the path in volume_path,
-- written down rather than joined for the same reason.
ALTER TABLE backups
    ADD COLUMN volume_id BIGINT REFERENCES app_volumes(id) ON DELETE SET NULL,
    ADD COLUMN volume_path TEXT NOT NULL DEFAULT '';

CREATE INDEX backups_volume ON backups (volume_id);

CREATE TABLE volume_backup_schedules (
    volume_id       BIGINT PRIMARY KEY REFERENCES app_volumes(id) ON DELETE CASCADE,
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
DROP TABLE volume_backup_schedules;
DROP INDEX backups_volume;
ALTER TABLE backups DROP COLUMN volume_path, DROP COLUMN volume_id;
