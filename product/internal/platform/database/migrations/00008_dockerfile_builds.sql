-- +goose Up

-- Where a building app gets its source. The repository and the ref are
-- not Dockerfile-specific — anything that builds from a repository needs
-- them — so they are not named for it.
ALTER TABLE apps ADD COLUMN source_repo TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN source_ref TEXT NOT NULL DEFAULT '';

-- Which file in that repository is the recipe, when there is a choice.
-- Empty means "Dockerfile", at the root.
ALTER TABLE apps ADD COLUMN source_dockerfile TEXT NOT NULL DEFAULT '';

-- A build's output. It lives on the deployment because that is where a
-- detached deploy's outcome lives: nobody is on the connection to be
-- told, and a build that failed is only explicable by what it printed.
ALTER TABLE deployments ADD COLUMN logs TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deployments DROP COLUMN logs;
ALTER TABLE apps DROP COLUMN source_dockerfile;
ALTER TABLE apps DROP COLUMN source_ref;
ALTER TABLE apps DROP COLUMN source_repo;
