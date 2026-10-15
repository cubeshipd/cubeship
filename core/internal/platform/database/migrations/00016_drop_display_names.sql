-- +goose Up

-- Organizations, projects and environments lose their display names.
--
-- An app never had one: its name *is* its slug, because the slug is a
-- path component of the app's registry reference and there was nothing
-- sensible to derive a second name from. Having one everywhere else made
-- the rule an exception rather than the rule — two ideas for one thing,
-- asked for at creation, drifting apart afterwards.
--
-- The derivation is what settled it. `slug.Title` turned `public-api`
-- into `Public Api`, deliberately dumb because anything cleverer would
-- be a dictionary. So the name was, almost always, the slug spelled
-- worse.
--
-- **Names typed by hand are lost.** The Down below restores the column
-- and fills it from the slug; it cannot bring back what somebody wrote.
ALTER TABLE organizations DROP COLUMN name;
ALTER TABLE projects DROP COLUMN name;
ALTER TABLE environments DROP COLUMN name;

-- +goose Down
ALTER TABLE organizations ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE environments ADD COLUMN name TEXT NOT NULL DEFAULT '';
UPDATE organizations SET name = slug;
UPDATE projects SET name = slug;
UPDATE environments SET name = slug;
