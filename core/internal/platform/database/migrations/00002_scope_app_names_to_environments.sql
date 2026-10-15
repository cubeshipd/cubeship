-- +goose Up
-- +goose StatementBegin

-- An app's name was unique across the whole instance, which made
-- environments useless as namespaces: the same app could not exist in
-- both "production" and "staging", and two organizations could not both
-- have an "api". Scope it to the environment it lives in.
ALTER TABLE apps DROP CONSTRAINT apps_name_key;
ALTER TABLE apps ADD CONSTRAINT apps_environment_id_name_key UNIQUE (environment_id, name);

-- The registry path has to carry the same scope, or two environments'
-- apps would push to one repository and each deploy would overwrite the
-- other. It becomes <registry host>/<org>/<project>/<environment>/<app>,
-- which is exactly the app's reference with the host in front.
--
-- Existing rows are rewritten rather than left pointing at a path that
-- no longer means anything: the push webhook matches on this column, so
-- a stale value would silently stop deploying. Images already pushed
-- stay where they are — the next push goes to the new path, which
-- `cubeship app get` prints.
UPDATE apps a
SET image = split_part(a.image, '/', 1)
         || '/' || o.slug
         || '/' || p.slug
         || '/' || e.slug
         || '/' || a.name
FROM organizations o, projects p, environments e
WHERE o.id = a.org_id AND p.id = a.project_id AND e.id = a.environment_id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE apps a
SET image = split_part(a.image, '/', 1) || '/' || o.slug || '/' || a.name
FROM organizations o
WHERE o.id = a.org_id;

ALTER TABLE apps DROP CONSTRAINT apps_environment_id_name_key;
ALTER TABLE apps ADD CONSTRAINT apps_name_key UNIQUE (name);

-- +goose StatementEnd
