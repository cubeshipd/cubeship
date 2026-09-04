-- +goose Up

-- Where a GitHub App is installed, and which Cubeship organization that
-- installation belongs to.
--
-- The installation is what grants access to a repository, so tying it to
-- an organization is what stops one tenant deploying another's private
-- code just by naming its URL.
CREATE TABLE github_installations (
    id              BIGSERIAL   PRIMARY KEY,
    org_id          BIGINT      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    installation_id BIGINT      NOT NULL,
    account_login   TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One GitHub account per organization, and one organization per
-- installation: both directions would otherwise make "which token does
-- this clone use" a question with no answer.
CREATE UNIQUE INDEX github_installations_org_account ON github_installations (org_id, lower(account_login));
CREATE UNIQUE INDEX github_installations_installation ON github_installations (installation_id);

-- +goose Down
DROP TABLE github_installations;
