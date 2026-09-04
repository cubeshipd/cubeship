-- +goose Up

-- The DNS accounts an organization manages its records through.
--
-- The credential belongs to the organization rather than to a domain or
-- an app: one account holds every zone in it, and rotating a token
-- should be one edit rather than one per name.
--
-- Two credentials for one provider are allowed — an account can
-- legitimately be split in two — so what is unique is the label, which
-- is the only thing here anyone chooses. Uniqueness of the provider
-- would have made the second account impossible to add.
CREATE TABLE dns_providers (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    provider   TEXT        NOT NULL,
    label      TEXT        NOT NULL,
    -- Route 53's access key id. Empty for Cloudflare, whose token is a
    -- single value with no name attached to it.
    username   TEXT        NOT NULL DEFAULT '',
    -- The secret half: Cloudflare's API token or Route 53's secret
    -- access key. Stored as given, because a provider takes the secret
    -- itself and a hash could not be sent to one.
    password   TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX dns_providers_org_label ON dns_providers (org_id, label);

-- +goose Down
DROP TABLE dns_providers;
