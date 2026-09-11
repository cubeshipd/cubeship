-- +goose Up
-- The description goes from the three things that have a slug and
-- nothing else: a project, an environment, an app.
--
-- It survived when display names were removed, on the grounds that it
-- says something a name never could. What it actually said, on every
-- instance, was nothing: it is optional, it is asked for once at
-- creation, it is read by whoever already knows what the thing is, and
-- the only screen that showed one was a project card. Behind it sat a
-- whole settings section per level whose reason to exist was editing
-- it — three forms, three PATCH bodies, three MCP arguments — for a
-- paragraph nobody writes twice.
--
-- What names something here is its slug, which is in the URL, in the
-- container's name and in the registry path. That was the decision
-- already made; this is the rest of it.
--
-- **The data goes with the column and does not come back.** The Down
-- adds the column again because a migration has to be reversible, and
-- whatever anybody had typed is gone by then — which is the honest
-- shape for this rather than a copy kept somewhere nothing reads.
ALTER TABLE apps DROP COLUMN description;
ALTER TABLE environments DROP COLUMN description;
ALTER TABLE projects DROP COLUMN description;

-- +goose Down
ALTER TABLE apps ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE environments ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN description TEXT NOT NULL DEFAULT '';
