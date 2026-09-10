-- +goose Up

-- What one copy of an app may take from the machine it runs on: CPU
-- quota in cores, and a hard memory ceiling in bytes.
--
-- **Per copy, not per app.** An app with three replicas and a one-core
-- limit may take three cores, which is the same arithmetic Kubernetes
-- does and the only one that survives the replica count changing.
--
-- **Zero is no limit**, which is what every container this instance has
-- ever run has had — one app with a leak could take the machine down
-- and nothing here stopped it.
--
-- The CPU column is numeric rather than an integer because half a core
-- is an ordinary answer on a box this size, and the memory column is
-- bytes rather than mebibytes because bytes is what the Engine takes
-- and a unit conversion in the database is a unit conversion somebody
-- eventually does twice.
ALTER TABLE apps
    ADD COLUMN cpu_limit NUMERIC(8, 2) NOT NULL DEFAULT 0,
    ADD COLUMN memory_limit BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE apps DROP COLUMN cpu_limit, DROP COLUMN memory_limit;
