-- +goose Up
-- +goose StatementBegin

-- One secret, stored once.
--
-- An AWS access key is the same key whether Route 53 writes a record
-- with it or ECR is pulled from with it, and before this each of those
-- asked for it separately. Three copies of one credential is three
-- places to rotate it and three chances to miss one.
--
-- What a credential can be used for follows from its provider and is
-- not stored: nothing here to tick, and nothing to get wrong. Where a
-- vendor issues two different kinds of secret — DigitalOcean's API
-- token and its Spaces keys are not the same string — those become two
-- providers, because that is what they are.
CREATE TABLE credentials (
    id       BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    -- What tells two apart, and the only thing here somebody chooses.
    -- It has to exist and has to be unique: the secret cannot be shown
    -- and the provider is not unique, so a label is the whole of how a
    -- person picks one out of a list.
    label    TEXT NOT NULL,
    -- The first half, and empty for a provider whose secret is one
    -- value. A token has no name attached to it.
    username TEXT NOT NULL DEFAULT '',
    -- The secret half. Stored as given, because a provider takes the
    -- secret itself and a hash could not be sent to one. Never returned
    -- by any endpoint.
    password TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Where a moved row came from, for the length of this migration
    -- only. Two things have to be re-pointed at ids this table has not
    -- issued yet, and matching them by label would be matching on the
    -- one column a person is free to make ambiguous.
    migrated_from TEXT
);

CREATE UNIQUE INDEX credentials_label ON credentials (lower(label));

-- The DNS providers move in whole. A DNS credential held nothing but a
-- provider, a label and a secret — which is exactly this table — so
-- there is nothing left of that table afterwards.
-- Route 53 is not a provider, it is a thing AWS runs: the same access
-- key reaches it and ECR, and keeping them under two names is what made
-- an operator store the key twice. This is the rename that makes one
-- credential cover both.
INSERT INTO credentials (provider, label, username, password, created_at, updated_at, migrated_from)
SELECT CASE WHEN provider = 'route53' THEN 'aws' ELSE provider END,
       label, username, password, created_at, updated_at, 'dns:' || id
FROM dns_providers;

-- The registries keep their row: a registry has configuration of its
-- own — which host, which namespace, which region — and that is the
-- part a credential is not. What it loses is the secret.
ALTER TABLE external_registries ADD COLUMN credential_id BIGINT;

-- Their labels are derived, because a registry credential never had
-- one: it was identified by the host it reaches. The prefix is what
-- keeps a derived label from colliding with one an operator chose, and
-- the id is the fallback for the case where it does anyway.
INSERT INTO credentials (provider, label, username, password, created_at, updated_at, migrated_from)
SELECT r.provider,
       CASE WHEN EXISTS (SELECT 1 FROM credentials c WHERE lower(c.label) = lower('Registry: ' || r.host))
            THEN 'Registry: ' || r.host || ' #' || r.id
            ELSE 'Registry: ' || r.host END,
       r.username, r.password, r.created_at, r.updated_at, 'registry:' || r.id
FROM external_registries r;

UPDATE external_registries r
SET credential_id = c.id
FROM credentials c
WHERE c.migrated_from = 'registry:' || r.id;

-- The instance's own DNS is a setting holding an id, and the id it held
-- belonged to a table that is about to be dropped.
UPDATE settings s
SET value = c.id::text
FROM credentials c
WHERE s.key = 'dns_provider_id' AND c.migrated_from = 'dns:' || s.value;

ALTER TABLE credentials DROP COLUMN migrated_from;

-- RESTRICT, not CASCADE. Deleting a credential that a registry still
-- authenticates with would leave a registry that cannot log in and a
-- deploy that fails for a reason nobody can see from the screen they
-- were on. The service turns this refusal into a sentence naming what
-- is still using it.
ALTER TABLE external_registries
    ALTER COLUMN credential_id SET NOT NULL,
    ADD CONSTRAINT external_registries_credential
        FOREIGN KEY (credential_id) REFERENCES credentials (id) ON DELETE RESTRICT;

-- The provider is the credential's now. Two copies of it would be two
-- things to keep in step, and the registry's copy is the one that could
-- disagree with the secret being used.
ALTER TABLE external_registries DROP COLUMN provider;
ALTER TABLE external_registries DROP COLUMN username;
ALTER TABLE external_registries DROP COLUMN password;

DROP TABLE dns_providers;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE dns_providers (
    id         BIGSERIAL PRIMARY KEY,
    provider   TEXT        NOT NULL,
    label      TEXT        NOT NULL,
    username   TEXT        NOT NULL DEFAULT '',
    password   TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX dns_providers_label ON dns_providers (label);

-- Every credential that can do DNS goes back to being a DNS provider.
-- Which one the instance was pointed at is recoverable, and so is which
-- credential a registry used. What is not is that an AWS key was *one*
-- credential doing both jobs: going back makes two copies of it again,
-- which is the thing this migration existed to end.
INSERT INTO dns_providers (provider, label, username, password, created_at, updated_at)
SELECT CASE WHEN provider = 'aws' THEN 'route53' ELSE provider END,
       label, username, password, created_at, updated_at
FROM credentials WHERE provider IN ('cloudflare', 'aws');

UPDATE settings s SET value = d.id::text
FROM dns_providers d
JOIN credentials c ON c.label = d.label
WHERE s.key = 'dns_provider_id' AND s.value = c.id::text;

ALTER TABLE external_registries ADD COLUMN provider TEXT NOT NULL DEFAULT 'generic';
ALTER TABLE external_registries ADD COLUMN username TEXT NOT NULL DEFAULT '';
ALTER TABLE external_registries ADD COLUMN password TEXT NOT NULL DEFAULT '';

UPDATE external_registries r
SET provider = c.provider, username = c.username, password = c.password
FROM credentials c WHERE c.id = r.credential_id;

ALTER TABLE external_registries DROP CONSTRAINT external_registries_credential;
ALTER TABLE external_registries DROP COLUMN credential_id;

DROP TABLE credentials;

-- +goose StatementEnd
