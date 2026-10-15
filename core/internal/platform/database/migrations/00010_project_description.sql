-- +goose Up
-- A project's description is the one place to say what it is for, which
-- its slug cannot: the slug is a path component and has to stay short
-- and machine-shaped. Empty rather than null, so every read gets a
-- string and no caller has to think about it.
ALTER TABLE projects ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE projects DROP COLUMN description;
