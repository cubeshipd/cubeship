-- +goose Up
-- A picture for a project, so a grid of them is something you recognise
-- rather than something you read.
--
-- **The column holds the media type, not the bytes.** The image itself
-- is a file under `<data dir>/projects/<id>`, beside everything else
-- this daemon owns that is not a row: the setup token, the update
-- state, the datastores' data directories. Postgres would hold it
-- perfectly well and would put a quarter of a megabyte of binary into
-- every backup, every dump and every read of the row that only wanted
-- to know the project's slug.
--
-- Empty is no picture, which is what every project starts as and most
-- will stay — the dashboard draws a mark instead. Storing the type is
-- what lets a listing say whether there is one without asking the disk
-- once per project, and what lets it be served back with the type it
-- arrived as.
ALTER TABLE projects ADD COLUMN image TEXT NOT NULL DEFAULT '';

-- +goose Down
-- The files stay. This migration did not write them and a rollback that
-- deleted somebody's pictures would be doing more than undoing itself.
ALTER TABLE projects DROP COLUMN image;
