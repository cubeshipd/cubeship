-- +goose Up
-- +goose StatementBegin

-- A credential is a secret with a label, and nothing else.
--
-- It used to carry a provider, and the provider was what said which API
-- the daemon speaks with it. That made a credential a thing you create
-- *per provider*: an API token you can only read once, stored under a
-- name that decides in advance the one job it may ever do. The point of
-- storing a secret centrally is the opposite — one token, entered once,
-- usable by whatever turns out to need it.
--
-- So the provider moves to the *use*. A registry already had a row of
-- its own and gets its provider back; DNS had none and gets one, which
-- is the table below. Both name the credential they authenticate with,
-- and one credential may be named by several.

-- A registry's provider is its own again. It was dropped in 00021 on
-- the grounds that the credential already said it, which is exactly the
-- coupling this migration undoes.
ALTER TABLE external_registries ADD COLUMN provider TEXT NOT NULL DEFAULT 'generic';

UPDATE external_registries r SET provider = c.provider
FROM credentials c WHERE c.id = r.credential_id;

ALTER TABLE external_registries ALTER COLUMN provider DROP DEFAULT;

-- Managing DNS through an account is no longer *being* a credential for
-- it: which API to speak — Route 53 or Cloudflare — is a fact about the
-- use, and one AWS key may reach Route 53 while the same key pulls from
-- ECR through a row in the table above.
CREATE TABLE dns_providers (
    id            BIGSERIAL PRIMARY KEY,
    provider      TEXT   NOT NULL,
    -- RESTRICT, like the registries': deleting a credential something
    -- still authenticates with would leave records nobody can write and
    -- a failure nobody can see from the screen they were on.
    credential_id BIGINT NOT NULL REFERENCES credentials (id) ON DELETE RESTRICT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per account per provider. A second would be the same
-- credential offered twice for the same job, which nothing could tell
-- apart.
CREATE UNIQUE INDEX dns_providers_account ON dns_providers (provider, credential_id);

-- Every credential that was doing DNS becomes a DNS provider on itself,
-- so an instance that was writing records keeps writing them.
--
-- "Was doing DNS" is not "could": 00021 gave an ECR login the provider
-- 'aws' too, and turning those into DNS providers would invent rows
-- nobody asked for. What tells them apart is what is standing on them
-- — a credential backing a registry was created for that registry —
-- and the one the instance is pointed at, which may be doing both.
INSERT INTO dns_providers (provider, credential_id, created_at, updated_at)
SELECT provider, id, created_at, updated_at
FROM credentials c
WHERE c.provider IN ('aws', 'cloudflare')
  AND (
    c.id::text = (SELECT value FROM settings WHERE key = 'dns_provider_id')
    OR NOT EXISTS (SELECT 1 FROM external_registries r WHERE r.credential_id = c.id)
  );

-- The setting held a credential id and now holds a DNS provider's.
UPDATE settings s SET value = d.id::text
FROM dns_providers d
WHERE s.key = 'dns_provider_id' AND d.credential_id::text = s.value;

ALTER TABLE credentials DROP COLUMN provider;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE credentials ADD COLUMN provider TEXT NOT NULL DEFAULT 'generic';

-- A credential's provider comes back from whatever was using it. One
-- used for both jobs cannot go back to being one row with one provider,
-- so DNS wins: it is the use that could not be expressed otherwise.
UPDATE credentials c SET provider = r.provider
FROM external_registries r WHERE r.credential_id = c.id;

UPDATE credentials c SET provider = d.provider
FROM dns_providers d WHERE d.credential_id = c.id;

UPDATE settings s SET value = d.credential_id::text
FROM dns_providers d
WHERE s.key = 'dns_provider_id' AND s.value = d.id::text;

ALTER TABLE credentials ALTER COLUMN provider DROP DEFAULT;

DROP TABLE dns_providers;

ALTER TABLE external_registries DROP COLUMN provider;

-- +goose StatementEnd
