-- +goose Up

-- Which Postgres extensions a managed database was created with.
--
-- Permanent, like the engine and the version above it, and for a
-- related reason: an extension decides which image the container runs,
-- and the image is what wrote — and can read — the data directory.
-- Adding one later would be a different image over the same files and a
-- CREATE EXTENSION nobody planned; removing one would be an image
-- missing a library the data already references.
--
-- JSONB rather than TEXT[] because every other list this schema keeps is
-- JSONB and `database/sql` has no array type of its own. '[]' rather
-- than NULL so every existing row reads as "no extensions" without a
-- reader that special-cases it — which is also the whole of what this
-- migration does to the databases already on an instance: they keep the
-- plain postgres image they came up on.
ALTER TABLE datastores
    ADD COLUMN extensions JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE datastores DROP COLUMN extensions;
