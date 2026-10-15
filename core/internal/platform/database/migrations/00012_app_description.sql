-- +goose Up
-- An app is created with a slug and a description and nothing else: what
-- it runs and where it is served are configured afterwards, inside the
-- app, by someone who has decided. The description is the one thing that
-- can be said at the moment of creation.
ALTER TABLE apps ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- The domain stays NOT NULL and becomes empty-able: it is no longer
-- required to have an app, only to deploy one, and an empty string says
-- "not configured yet" without making every read think about null.
ALTER TABLE apps ALTER COLUMN domain SET DEFAULT '';

-- +goose Down
ALTER TABLE apps DROP COLUMN description;
ALTER TABLE apps ALTER COLUMN domain DROP DEFAULT;
