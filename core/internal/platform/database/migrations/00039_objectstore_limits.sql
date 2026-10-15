-- +goose Up

-- What a managed store's container may take from the machine, in the
-- same two numbers an app's and a database's carry.
--
-- **Only a managed store has one.** A linked store is somebody else's
-- server: there is no container here to cap and never will be, which is
-- why the API refuses a limit on one rather than storing a number that
-- would do nothing. The columns are on the shared table because the two
-- kinds are one table — see internal/objectstore — and a linked store
-- simply leaves them at zero.
ALTER TABLE object_stores
    ADD COLUMN cpu_limit NUMERIC(8, 2) NOT NULL DEFAULT 0,
    ADD COLUMN memory_limit BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE object_stores DROP COLUMN cpu_limit, DROP COLUMN memory_limit;
