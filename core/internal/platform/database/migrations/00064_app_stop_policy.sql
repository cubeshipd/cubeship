-- +goose Up
ALTER TABLE apps
  ADD COLUMN stop_grace_period BIGINT NOT NULL DEFAULT 10,
  ADD COLUMN stop_signal TEXT NOT NULL DEFAULT 'SIGTERM';

-- +goose Down
ALTER TABLE apps
  DROP COLUMN stop_signal,
  DROP COLUMN stop_grace_period;
