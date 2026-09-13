-- +goose Up
-- Which tag an app runs, and whether it follows the registry at all.
--
-- Empty is what every app is today and keeps meaning what it meant:
-- on Cubeship's own registry, run whatever is pushed — the push is the
-- deploy; anywhere else, `latest`. A tag here is the app pinned to it,
-- deployed when somebody asks.
--
-- One column rather than a tag beside an autodeploy flag, because the
-- two could then disagree: an app pinned to v1.0 and told to follow
-- every push has no answer. Off is the value, the way autoscale_max = 0
-- is what autoscaling off is.
ALTER TABLE apps ADD COLUMN source_tag TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE apps DROP COLUMN source_tag;
