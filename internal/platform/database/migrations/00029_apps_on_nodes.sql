-- +goose Up

-- Which machine an app runs on.
--
-- Backfilled to the control plane rather than defaulted, because the
-- default would have to name a row by a number this file cannot know.
-- Every app that exists today runs where the daemon does — there was
-- nowhere else — so that is not a guess, it is the fact being written
-- down for the first time.
ALTER TABLE apps ADD COLUMN node_id BIGINT REFERENCES nodes (id) ON DELETE RESTRICT;

UPDATE apps SET node_id = (SELECT id FROM nodes WHERE control_plane);

ALTER TABLE apps ALTER COLUMN node_id SET NOT NULL;

-- RESTRICT rather than CASCADE or SET NULL, and it is the whole reason
-- the foreign key is here: a machine with apps on it cannot be removed
-- from the cluster without somebody deciding where those apps go. The
-- alternatives are an app pointing at a machine that does not exist, or
-- an app quietly moved to nowhere.

CREATE INDEX apps_node ON apps (node_id);

-- +goose Down
ALTER TABLE apps DROP COLUMN node_id;
