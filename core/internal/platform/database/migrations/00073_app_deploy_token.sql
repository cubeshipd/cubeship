-- Every app answers a deploy webhook at /hooks/deploy/<token>, so a CI
-- that pushes to a registry Cubeship does not run can say "deploy this".
-- The default is volatile, so ADD COLUMN evaluates it once per existing
-- row, every INSERT gets its own, and rotating is SET deploy_token =
-- DEFAULT: one generator, 256 bits from three random UUIDs. Kept in
-- plain text because the dashboard shows the URL again, as the GitHub
-- webhook secret is.
-- +goose Up
ALTER TABLE apps ADD COLUMN deploy_token TEXT NOT NULL DEFAULT encode(sha256(
    uuid_send(gen_random_uuid()) || uuid_send(gen_random_uuid()) || uuid_send(gen_random_uuid())
), 'hex');
CREATE UNIQUE INDEX apps_deploy_token ON apps (deploy_token);

-- +goose Down
DROP INDEX apps_deploy_token;
ALTER TABLE apps DROP COLUMN deploy_token;
