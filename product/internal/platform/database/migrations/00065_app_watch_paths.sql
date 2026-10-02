-- +goose Up
ALTER TABLE apps
  ADD COLUMN watch_paths JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN ignore_paths JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE apps
  DROP COLUMN ignore_paths,
  DROP COLUMN watch_paths;
