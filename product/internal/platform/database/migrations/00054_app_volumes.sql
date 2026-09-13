-- +goose Up

-- A directory on one machine's disk, mounted into an app's container at
-- `path`, that outlives the container.
--
-- node_id is the machine the data is on, and it never changes: data does
-- not move between machines, which is why an app with a volume runs as
-- one copy on that one. RESTRICT on the node for the reason app_nodes
-- has it — a machine holding somebody's data cannot leave the cluster
-- without somebody deciding what happens to it.
--
-- The row goes with its app. Whether the directory goes too is asked of
-- whoever deletes it, and a kept one is found by what is on disk.
CREATE TABLE app_volumes (
    id         BIGSERIAL PRIMARY KEY,
    app_id     BIGINT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    node_id    BIGINT NOT NULL REFERENCES nodes (id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (app_id, path)
);

CREATE INDEX app_volumes_app ON app_volumes (app_id);

-- +goose Down
DROP TABLE app_volumes;
