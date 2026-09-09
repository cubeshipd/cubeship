-- +goose Up

-- Which machines an app runs on, and what is running on each.
--
-- One row per machine, and an app with one machine has one row — the
-- shape does not change when a second is added, which is the point.
-- Desired and actual are the same row: the row existing means "run this
-- app here", and the container columns are what is actually there.
-- Splitting them into two tables would be two answers to "is it up".
CREATE TABLE app_nodes (
    app_id         BIGINT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    node_id        BIGINT NOT NULL REFERENCES nodes (id) ON DELETE RESTRICT,
    -- The container serving this app on this machine, and what it is
    -- called. The name is not decoration: it is the address every other
    -- machine reaches this replica at over the mesh, which is what the
    -- edge's load balancer is built out of.
    container_id   TEXT NOT NULL DEFAULT '',
    container_name TEXT NOT NULL DEFAULT '',
    deployment_id  BIGINT,
    -- Whether that container carries Traefik routers for the app's own
    -- names.
    --
    -- A container keeps the labels it was created with, so this is a
    -- fact about the past that nothing else can recover: an app that
    -- was on two machines when it was last deployed has containers that
    -- route nothing, and it stays that way after it is scaled back to
    -- one. Without this column, scaling down would take a name off the
    -- internet until somebody happened to redeploy.
    routed         BOOLEAN NOT NULL DEFAULT true,
    status         TEXT NOT NULL DEFAULT 'pending',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (app_id, node_id)
);

-- RESTRICT on the node, like the column it replaces: a machine with
-- apps on it cannot leave the cluster without somebody deciding where
-- those apps go.

CREATE INDEX app_nodes_node ON app_nodes (node_id);

INSERT INTO app_nodes (app_id, node_id, container_id, status)
SELECT id, node_id, container_id, status FROM apps;

-- The container an app has been running since before this table is one
-- whose *name* nothing wrote down — the column did not exist. It is
-- left empty, which means that replica is not offered as a load
-- balancer backend until its next deploy names one. An app with one
-- machine never needs it: its own machine routes to it by label.

ALTER TABLE apps DROP COLUMN container_id;
ALTER TABLE apps DROP COLUMN status;

-- node_id stays and its meaning narrows: it is no longer "the machine
-- this app runs on" — app_nodes answers that — it is **the machine
-- whose Traefik serves this app's names**.
--
-- One machine rather than all of them, and the reason is the
-- certificate. A machine that routes a name asks Let's Encrypt for it,
-- and a machine the name does not resolve to fails that validation
-- every time and spends a rate limit shared with everyone else under
-- that domain. So exactly one machine answers for a name, it is the one
-- the DNS record points at, and the balancing happens behind it.
COMMENT ON COLUMN apps.node_id IS 'the machine whose edge serves this app''s names; app_nodes is where it runs';

-- +goose Down
ALTER TABLE apps ADD COLUMN container_id TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN status TEXT NOT NULL DEFAULT 'pending';

UPDATE apps a SET container_id = n.container_id, status = n.status
FROM app_nodes n WHERE n.app_id = a.id AND n.node_id = a.node_id;

DROP TABLE app_nodes;
