-- +goose Up
-- +goose StatementBegin

-- Organizations are gone. Cubeship runs one instance on one VPS, and a
-- tenant boundary inside it was a level everybody had to name and nobody
-- could use: one organization existed, every screen asked which, and
-- every app's registry path carried a component that was always the same
-- word.
--
-- What the organization actually held was a role, so the role moves onto
-- the user. Admin and member keep their meanings exactly — a member
-- deploys published images, an admin also builds source on this host —
-- they are simply a property of the account now rather than of a
-- membership in it.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'member';

-- A super-admin becomes an admin; so does anyone who was an admin
-- anywhere. Nobody loses a permission they had, and the one thing
-- super-admin could do that an org admin could not — create and delete
-- organizations — no longer exists.
UPDATE users SET role = 'admin'
WHERE is_super_admin
   OR id IN (SELECT user_id FROM memberships WHERE role = 'admin');

ALTER TABLE users DROP COLUMN is_super_admin;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'member'));

-- Dropping org_id takes every constraint and index that named it with
-- it, so each table's scope is rebuilt afterwards. What was unique
-- within an organization becomes unique on the instance — which is what
-- a URL and a registry path under it now assume, and what they already
-- were on a box with one tenant.
--
-- Two rows that only differed by organization would collide here, and
-- the migration would abort rather than pick one. Postgres has
-- transactional DDL, so nothing is half-applied.
ALTER TABLE projects DROP COLUMN org_id;
ALTER TABLE projects ADD CONSTRAINT projects_slug_key UNIQUE (slug);

ALTER TABLE apps DROP COLUMN org_id;

ALTER TABLE external_registries DROP COLUMN org_id;
CREATE UNIQUE INDEX external_registries_host ON external_registries (host);

ALTER TABLE dns_providers DROP COLUMN org_id;
CREATE UNIQUE INDEX dns_providers_label ON dns_providers (label);

ALTER TABLE github_installations DROP COLUMN org_id;
CREATE UNIQUE INDEX github_installations_account ON github_installations (lower(account_login));

DROP TABLE memberships;
DROP TABLE organizations;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Irreversible in the way that matters: which organization each project,
-- app and credential belonged to is not recorded anywhere once the
-- column is gone. Down rebuilds the shape and puts everything in one
-- organization, which is the only answer left.
CREATE TABLE organizations (
	id BIGSERIAL PRIMARY KEY,
	slug TEXT NOT NULL UNIQUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO organizations (slug) VALUES ('default');

CREATE TABLE memberships (
	user_id BIGINT NOT NULL REFERENCES users(id),
	org_id BIGINT NOT NULL REFERENCES organizations(id),
	role TEXT NOT NULL,
	PRIMARY KEY (user_id, org_id)
);
INSERT INTO memberships (user_id, org_id, role)
SELECT u.id, o.id, u.role FROM users u, organizations o;

ALTER TABLE users ADD COLUMN is_super_admin BOOLEAN NOT NULL DEFAULT false;
UPDATE users SET is_super_admin = true WHERE role = 'admin';
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users DROP COLUMN role;

ALTER TABLE projects ADD COLUMN org_id BIGINT REFERENCES organizations(id);
UPDATE projects SET org_id = (SELECT id FROM organizations LIMIT 1);
ALTER TABLE projects ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE projects DROP CONSTRAINT projects_slug_key;
ALTER TABLE projects ADD CONSTRAINT projects_org_id_slug_key UNIQUE (org_id, slug);

ALTER TABLE apps ADD COLUMN org_id BIGINT REFERENCES organizations(id);
UPDATE apps SET org_id = (SELECT id FROM organizations LIMIT 1);
ALTER TABLE apps ALTER COLUMN org_id SET NOT NULL;

DROP INDEX external_registries_host;
ALTER TABLE external_registries ADD COLUMN org_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE;
UPDATE external_registries SET org_id = (SELECT id FROM organizations LIMIT 1);
ALTER TABLE external_registries ALTER COLUMN org_id SET NOT NULL;
CREATE UNIQUE INDEX external_registries_org_host ON external_registries (org_id, host);

DROP INDEX dns_providers_label;
ALTER TABLE dns_providers ADD COLUMN org_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE;
UPDATE dns_providers SET org_id = (SELECT id FROM organizations LIMIT 1);
ALTER TABLE dns_providers ALTER COLUMN org_id SET NOT NULL;
CREATE UNIQUE INDEX dns_providers_org_label ON dns_providers (org_id, label);

DROP INDEX github_installations_account;
ALTER TABLE github_installations ADD COLUMN org_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE;
UPDATE github_installations SET org_id = (SELECT id FROM organizations LIMIT 1);
ALTER TABLE github_installations ALTER COLUMN org_id SET NOT NULL;
CREATE UNIQUE INDEX github_installations_org_account ON github_installations (org_id, lower(account_login));

-- +goose StatementEnd
