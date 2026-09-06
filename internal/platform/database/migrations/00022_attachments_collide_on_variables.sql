-- An attachment collides with another when they name the same
-- variables, and the prefix is only half of that name.
--
-- `(app_id, prefix)` said an app could hold one database at each
-- prefix, which is true of Postgres, MySQL and MariaDB — they all write
-- `<prefix>DATABASE_URL` and its parts, so two of them at one prefix
-- would be one variable with two values. Redis writes `REDIS_URL` and
-- MongoDB writes `MONGO_URL`. Attaching a cache beside a database is
-- the ordinary thing to want, and it was refused for a conflict that
-- does not exist.
--
-- So the stem travels on the attachment, and uniqueness is over the
-- whole variable name. It is stored rather than joined because a unique
-- index cannot read another table, and it is safe to store because an
-- engine is fixed for the life of a datastore — see the module's notes
-- on what cannot be changed after creation.

-- +goose Up
ALTER TABLE datastore_attachments ADD COLUMN stem TEXT NOT NULL DEFAULT 'DATABASE';

-- The mapping is spelled out here rather than derived, because a
-- migration is what the tables looked like on the day it ran. The
-- daemon writes the stem from `Engine.VarStem` from now on.
UPDATE datastore_attachments t
SET stem = CASE d.engine
    WHEN 'redis' THEN 'REDIS'
    WHEN 'mongodb' THEN 'MONGO'
    ELSE 'DATABASE'
END
FROM datastores d
WHERE d.id = t.datastore_id;

DROP INDEX datastore_attachments_app_prefix;
CREATE UNIQUE INDEX datastore_attachments_app_vars
    ON datastore_attachments (app_id, prefix, stem);

-- +goose Down
DROP INDEX datastore_attachments_app_vars;
ALTER TABLE datastore_attachments DROP COLUMN stem;
CREATE UNIQUE INDEX datastore_attachments_app_prefix
    ON datastore_attachments (app_id, prefix);
