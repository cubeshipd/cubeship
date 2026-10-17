-- +goose Up
ALTER TABLE metric_samples ADD COLUMN container_started_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE metric_samples DROP COLUMN container_started_at;
