-- +goose Up
-- Same reason as a project's: the slug is a path component of every app
-- reference in the environment, so it has to stay short and
-- machine-shaped, and there is nowhere else to say what the environment
-- is for. Empty rather than null, so every read gets a string.
ALTER TABLE environments ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE environments DROP COLUMN description;
