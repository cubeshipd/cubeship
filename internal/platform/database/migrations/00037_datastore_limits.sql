-- +goose Up

-- What a database's container may take from the machine it runs on, in
-- the same two numbers an app's carries: CPU quota in cores, and a hard
-- memory ceiling in bytes. Zero is no limit, which is what every
-- datastore this instance has ever provisioned has had.
--
-- A database is the container on a box this size most worth capping. An
-- app that leaks is one app; a Postgres that takes every page of memory
-- on the machine takes the daemon, the proxy and everything else with
-- it — and unlike an app it is not restarted by a deploy somebody was
-- about to do anyway.
ALTER TABLE datastores
    ADD COLUMN cpu_limit NUMERIC(8, 2) NOT NULL DEFAULT 0,
    ADD COLUMN memory_limit BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE datastores DROP COLUMN cpu_limit, DROP COLUMN memory_limit;
