-- +goose Up
ALTER TABLE apps
    ADD COLUMN predeploy_command JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN predeploy_timeout INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE apps
    DROP COLUMN predeploy_timeout,
    DROP COLUMN predeploy_command;
