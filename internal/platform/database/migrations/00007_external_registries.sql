-- +goose Up

-- Where an app with source 'external' pulls from: a full image reference
-- in somebody else's registry, minus the tag. An app on the embedded
-- registry derives its path from its reference and leaves this empty.
ALTER TABLE apps ADD COLUMN source_image TEXT NOT NULL DEFAULT '';

-- Credentials for pulling from a registry Cubeship does not run.
--
-- They belong to the organization, not to an app: one DigitalOcean or
-- ECR login covers every image in it, and rotating a password should be
-- one edit rather than one per app.
CREATE TABLE external_registries (
    id          BIGSERIAL PRIMARY KEY,
    org_id      BIGINT      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    host        TEXT        NOT NULL,
    username    TEXT        NOT NULL,
    password    TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One credential per host per organization: two would make "which one
-- does this pull use" a question with no answer.
CREATE UNIQUE INDEX external_registries_org_host ON external_registries (org_id, host);
CREATE UNIQUE INDEX external_registries_org_name ON external_registries (org_id, name);

-- +goose Down
DROP TABLE external_registries;
ALTER TABLE apps DROP COLUMN source_image;
