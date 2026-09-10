-- +goose Up

-- Every name arrives at the control plane, and it routes to whichever
-- copy of the app should answer — on this machine or on any other,
-- over the mesh, by container name.
--
-- **The certificate is what decides this.** A machine that routes a
-- name asks Let's Encrypt for it over TLS-ALPN on its own :443, so a
-- machine the record does not resolve to fails that challenge for ever
-- and spends a limit shared by everyone under that domain. An edge per
-- app meant a DNS record per app, repointed by hand every time an app
-- moved, and a certificate store on every box. One front door means one
-- record, one store, and an app that moves without anybody touching
-- DNS.
--
-- Traefik is a load balancer. It was already the thing in front of
-- every container on this box; this is the same job over a network that
-- now exists.
--
-- What it costs is honest: the control plane is where all app traffic
-- arrives, so it is down when that box is. Before, an app on a worker
-- kept serving a control plane that had died — at the price of a record
-- per app and a certificate per machine, and of every request for an
-- app on two machines being served by whichever one the record happened
-- to name.
ALTER TABLE apps DROP COLUMN node_id;

-- A container carries no Traefik router any more: the file the control
-- plane writes is the only thing that routes a name, so there is no
-- second answer to remember.
ALTER TABLE app_nodes DROP COLUMN routed;

-- +goose Down
ALTER TABLE app_nodes ADD COLUMN routed BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE apps ADD COLUMN node_id BIGINT REFERENCES nodes (id) ON DELETE RESTRICT;
UPDATE apps a SET node_id = (
    SELECT node_id FROM app_nodes r WHERE r.app_id = a.id ORDER BY r.node_id LIMIT 1);
ALTER TABLE apps ALTER COLUMN node_id SET NOT NULL;
