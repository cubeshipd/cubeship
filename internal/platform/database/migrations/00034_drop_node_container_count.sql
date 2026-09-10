-- +goose Up

-- How many containers a machine is running was a number nobody could
-- act on. What is worth knowing about a machine is *what* is running
-- there — which one is eating the box, which app is where — and a count
-- answers none of that while looking like it answers something.
--
-- It also cost a round trip's worth of honesty: the agent counted only
-- the containers carrying this instance's label, so the number was
-- never "what is on that box" either.
ALTER TABLE nodes DROP COLUMN containers;

-- +goose Down
ALTER TABLE nodes ADD COLUMN containers INTEGER NOT NULL DEFAULT 0;
