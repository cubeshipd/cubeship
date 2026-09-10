-- +goose Up

-- Whether this app follows the cluster: run on **every machine there
-- is**, now and whenever one is added, rather than on a set somebody
-- keeps in step by hand.
--
-- It is a switch rather than a second way of placing things. The set of
-- machines and the number of copies already say where an app runs and
-- how many of it; what they cannot say is "wherever the cluster goes",
-- which is what somebody means by distributing an app across the
-- servers. Off, an app runs on the machines it was placed on and a new
-- server changes nothing about it. On, it is re-spread the moment the
-- cluster changes shape.
--
-- Off is the default and what every app has, because it is what every
-- app has always done.
ALTER TABLE apps ADD COLUMN spread BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE apps DROP COLUMN spread;
