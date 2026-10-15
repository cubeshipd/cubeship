-- +goose Up

-- The machines this instance is made of.
--
-- One row per box, including this one: the control plane is a node
-- with `control_plane` set, seeded below, and it is in the table rather
-- than implied because everything that will later be placed on a node
-- has to be placeable here. A screen listing "the other servers" is a
-- screen that cannot answer where an app runs.
CREATE TABLE nodes (
    id            BIGSERIAL PRIMARY KEY,
    -- The name, and the whole of it. Permanent, like every other slug
    -- here: what gets configured against a node is configured against
    -- this word.
    slug          TEXT NOT NULL UNIQUE,
    description   TEXT NOT NULL DEFAULT '',
    control_plane BOOLEAN NOT NULL DEFAULT false,

    -- What a worker authenticates with. Only its hash is stored, like
    -- an API key's — a node is not a person, so this is not one, and it
    -- reaches a different middleware. NULL on the control plane, which
    -- does not dial itself.
    token_hash    TEXT UNIQUE,

    -- Everything below is reported by the agent and overwritten on
    -- every pass. None of it is configuration: it is what the box said
    -- about itself the last time it called.
    address            TEXT   NOT NULL DEFAULT '',
    version            TEXT   NOT NULL DEFAULT '',
    cores              INTEGER NOT NULL DEFAULT 0,
    memory_total_bytes BIGINT NOT NULL DEFAULT 0,
    disk_total_bytes   BIGINT NOT NULL DEFAULT 0,
    -- The newest reading, so a listing shows load without a live call
    -- to every machine in the cluster. NULL until one has been taken —
    -- zero would be a reading.
    cpu_percent        DOUBLE PRECISION,
    memory_bytes       BIGINT,
    disk_bytes         BIGINT,
    containers         INTEGER NOT NULL DEFAULT 0,

    -- When the agent last called. NULL on a node that has never
    -- connected, which is what tells "not yet" from "not any more" —
    -- the status is derived from this rather than stored, so nothing
    -- has to run on a timer to keep it true.
    last_seen_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- There is one control plane, and it is this instance. A second row
-- claiming to be one would be two answers to "who decides".
CREATE UNIQUE INDEX nodes_one_control_plane ON nodes (control_plane) WHERE control_plane;

-- This machine. It exists from the first migration onwards because it
-- existed before there was a table: every container this instance runs
-- today runs here.
INSERT INTO nodes (slug, description, control_plane)
VALUES ('control-plane', 'This machine, which runs the dashboard, the registry and the builder.', true);

-- +goose Down
DROP TABLE nodes;
