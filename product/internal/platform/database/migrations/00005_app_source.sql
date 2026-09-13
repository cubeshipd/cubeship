-- +goose Up
-- +goose StatementBegin

-- Where an app's image comes from. Today every app is pushed to the
-- embedded registry, which is why that is the default and the only value
-- the daemon accepts; building from a repository and pulling from a
-- third-party registry are the reason the column exists.
--
-- The configuration each source needs — a repository and branch, a
-- credential — is deliberately not here yet. Its shape belongs to the
-- source that defines it, and guessing at it now would mean guessing
-- wrong.
ALTER TABLE apps ADD COLUMN source TEXT NOT NULL DEFAULT 'registry';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE apps DROP COLUMN source;

-- +goose StatementEnd
