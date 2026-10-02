-- +goose Up
ALTER TABLE deployments
    ADD COLUMN phase TEXT NOT NULL DEFAULT 'queued',
    ADD COLUMN started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN finished_at TIMESTAMPTZ,
    ADD COLUMN cancelled BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE deployments
    DROP COLUMN cancelled,
    DROP COLUMN finished_at,
    DROP COLUMN started_at,
    DROP COLUMN phase;
