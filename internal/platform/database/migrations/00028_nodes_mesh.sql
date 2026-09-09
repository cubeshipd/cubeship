-- +goose Up

-- What the swarm calls this machine, as the agent last reported it.
--
-- Empty means it is not on the cluster's private network: it has not
-- joined yet, or it could not. The id rather than a boolean because the
-- one question anybody asks after "is it on the mesh" is "which of
-- these is it in `docker node ls`", and a yes/no cannot answer that.
ALTER TABLE nodes ADD COLUMN mesh_node_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN mesh_node_id;
