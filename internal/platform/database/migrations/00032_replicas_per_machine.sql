-- +goose Up

-- More than one copy of an app on one machine.
--
-- The row is still what says "run a copy here", and now a machine can
-- have several: the ordinal tells them apart. Everything that existed
-- becomes ordinal 1, which is not a guess — a machine ran exactly one
-- copy, because there was no way to ask for a second.
ALTER TABLE app_nodes ADD COLUMN ordinal INT NOT NULL DEFAULT 1;

ALTER TABLE app_nodes DROP CONSTRAINT app_nodes_pkey;
ALTER TABLE app_nodes ADD PRIMARY KEY (app_id, node_id, ordinal);

-- **How many copies is a number per app, not per machine**, spread over
-- the machines it runs on. One number is how people think about scale —
-- "run four of these" — and it keeps two decisions from becoming three:
-- which machines, how many copies, and then a count per machine that
-- has to be kept in step with the first two by hand.
--
-- It is not stored. The rows are the answer: four rows is four copies,
-- and where they sit is the spread. A column beside them would be a
-- second place to read the same fact from, and the two would disagree
-- the first time a machine was added.
--
-- What is given up is a different count per machine — three on the big
-- box and one on the small one. That is a real thing to want on
-- machines that are not alike, and it is not representable here.

-- +goose Down
DELETE FROM app_nodes WHERE ordinal > 1;
ALTER TABLE app_nodes DROP CONSTRAINT app_nodes_pkey;
ALTER TABLE app_nodes ADD PRIMARY KEY (app_id, node_id);
ALTER TABLE app_nodes DROP COLUMN ordinal;
