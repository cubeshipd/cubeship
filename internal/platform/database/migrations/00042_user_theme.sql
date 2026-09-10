-- +goose Up

-- Which set of colours this person sees the dashboard in.
--
-- **On `users` rather than in a browser**, because it is a fact about
-- the person and not about the machine they happened to open it on: a
-- laptop and a desktop showing one admin two different interfaces is
-- the kind of small wrongness nobody reports and everybody notices.
-- The browser keeps a copy so the first paint has an answer before the
-- daemon has been asked — see the dashboard — but this row is what is
-- true.
--
-- Empty is the default palette. Stored as a name rather than colours:
-- the palettes are the product's, and a hex value in a column would be
-- one nothing could restyle when the interface moves on.
ALTER TABLE users ADD COLUMN theme TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN theme;
