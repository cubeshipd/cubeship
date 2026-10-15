-- +goose Up
ALTER TABLE deployments
    ADD COLUMN git_sha TEXT NOT NULL DEFAULT '',
    ADD COLUMN git_branch TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deployments
    DROP COLUMN git_branch,
    DROP COLUMN git_sha;
