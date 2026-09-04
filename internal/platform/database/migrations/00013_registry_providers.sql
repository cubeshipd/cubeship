-- +goose Up

-- A registry credential is now shaped by which registry it is for.
--
-- The display name goes: a credential is identified by the registry it
-- reaches, and one per host per organization was already the rule. A
-- second name to keep in step bought nothing.
ALTER TABLE external_registries ADD COLUMN provider TEXT NOT NULL DEFAULT 'generic';

-- Namespace is the path segment between the host and the image, where a
-- provider has one: DigitalOcean's registry name, for instance. It is
-- not part of the host, so it does not take part in matching — it is
-- what lets an image path be offered rather than typed.
ALTER TABLE external_registries ADD COLUMN namespace TEXT NOT NULL DEFAULT '';

-- Region is AWS's. An ECR host carries the account id and the region,
-- and the account id is discovered; the region cannot be.
ALTER TABLE external_registries ADD COLUMN region TEXT NOT NULL DEFAULT '';

DROP INDEX external_registries_org_name;
ALTER TABLE external_registries DROP COLUMN name;

-- +goose Down
ALTER TABLE external_registries ADD COLUMN name TEXT NOT NULL DEFAULT '';
UPDATE external_registries SET name = host;
CREATE UNIQUE INDEX external_registries_org_name ON external_registries (org_id, name);
ALTER TABLE external_registries DROP COLUMN region;
ALTER TABLE external_registries DROP COLUMN namespace;
ALTER TABLE external_registries DROP COLUMN provider;
