-- +goose Up

-- How many copies of an app were *asked for*, which is not the same
-- fact as how many there are.
--
-- The rows in app_nodes are how many there are, and they cannot express
-- the answer almost every app gives: "one per machine, whatever that
-- number turns out to be". Reading the intent off the row count instead
-- meant taking a machine away from an app running one copy on each of
-- two silently left two copies on the survivor — somebody moved an app
-- off a box and got a second copy on the other one.
--
-- **Zero means one per machine**, and it is the default and what every
-- app that exists has. A number means that many, spread over whatever
-- machines the app is on, and it survives adding and removing machines
-- because it is a decision somebody made rather than a count somebody
-- happened to end up with.
ALTER TABLE apps ADD COLUMN scale INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE apps DROP COLUMN scale;
