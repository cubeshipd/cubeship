-- +goose Up
ALTER TABLE host_samples ADD COLUMN node_id BIGINT NOT NULL DEFAULT 0;
-- Zero preserves the existing control-plane history. Worker ids are never reused.
CREATE UNIQUE INDEX host_samples_node_at ON host_samples (node_id, at);
CREATE TABLE host_reports (
    node_id BIGINT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    report JSONB NOT NULL
);

-- +goose Down
DELETE FROM host_samples WHERE node_id <> 0;
DROP TABLE host_reports;
DROP INDEX host_samples_node_at;
ALTER TABLE host_samples DROP COLUMN node_id;
