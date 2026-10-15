-- +goose Up
ALTER TABLE apps ADD COLUMN build_args JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE apps DROP COLUMN build_args;
