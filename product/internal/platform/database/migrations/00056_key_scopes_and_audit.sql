-- +goose Up
-- What a key may do, under its owner's role: read, deploy, or everything
-- the owner can. all_projects is a column rather than "no rows below":
-- a scoped key whose projects were all deleted must reach nothing, not
-- everything.
ALTER TABLE api_keys
    ADD COLUMN access TEXT NOT NULL DEFAULT 'full',
    ADD COLUMN all_projects BOOLEAN NOT NULL DEFAULT true;

CREATE TABLE api_key_projects (
    key_id     BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    PRIMARY KEY (key_id, project_id)
);

-- Who changed what. Names are written down rather than joined: the row
-- has to outlive the account, the key and the thing it names.
CREATE TABLE audit_events (
    id         BIGSERIAL PRIMARY KEY,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    username   TEXT NOT NULL,
    via        TEXT NOT NULL,
    key_id     BIGINT,
    key_name   TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    outcome    TEXT NOT NULL,
    status     INT NOT NULL DEFAULT 0,
    detail     TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX audit_events_at ON audit_events (at);
CREATE INDEX audit_events_username ON audit_events (username, id);

-- +goose Down
DROP TABLE audit_events;
DROP TABLE api_key_projects;
ALTER TABLE api_keys DROP COLUMN all_projects, DROP COLUMN access;
