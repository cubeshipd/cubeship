-- +goose Up

-- A port of an app's container published on a host port of the control
-- plane, for a protocol that is not HTTP: SSH into a Git server, a game
-- server, a broker.
--
-- host_port is unique across apps because a host port is bound once. It
-- is not checked against datastores' and stores' exposed ports here —
-- those are other tables — so the service asks for their union before it
-- writes, and Docker refuses the bind for anything nobody wrote down.
--
-- The row goes with its app; there is nothing to keep.
CREATE TABLE app_tcp_ports (
    id             BIGSERIAL PRIMARY KEY,
    app_id         BIGINT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    container_port INTEGER NOT NULL CHECK (container_port BETWEEN 1 AND 65535),
    host_port      INTEGER NOT NULL CHECK (host_port BETWEEN 1024 AND 65535),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT app_tcp_ports_host_port UNIQUE (host_port),
    CONSTRAINT app_tcp_ports_container_port UNIQUE (app_id, container_port)
);

-- +goose Down
DROP TABLE app_tcp_ports;
