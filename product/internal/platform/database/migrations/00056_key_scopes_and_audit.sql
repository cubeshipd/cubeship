-- +goose Up
-- What a member, or an API key, may reach: a name, and a list of grants
-- — one per kind of resource, each a level, whether it reads secrets,
-- and which of them (null for every one).
--
-- Grants are JSONB rather than rows because a role is edited as one
-- thing, from one screen, and read as one thing on every request.
CREATE TABLE access_roles (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    grants      JSONB NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A member's role; null is the built-in member default. An admin's is
-- ignored. RESTRICT, because the service refuses deleting a role in use
-- and a foreign key is what makes that true under a race.
ALTER TABLE users ADD COLUMN access_role_id BIGINT REFERENCES access_roles(id) ON DELETE RESTRICT;

-- A key's role narrows its owner's access; null is all of it.
ALTER TABLE api_keys ADD COLUMN access_role_id BIGINT REFERENCES access_roles(id) ON DELETE RESTRICT;

INSERT INTO access_roles (name, description, grants) VALUES
('Read only', 'Sees everything a member can be given, changes nothing, and reads no secret.', '[
  {"resource":"projects","level":"view","items":null},
  {"resource":"apps","level":"view","items":null},
  {"resource":"domains","level":"view","items":null},
  {"resource":"databases","level":"view","items":null},
  {"resource":"storage","level":"view","items":null},
  {"resource":"servers","level":"view","items":null},
  {"resource":"templates","level":"view","items":null},
  {"resource":"backups","level":"view","items":null},
  {"resource":"registry","level":"view","items":null},
  {"resource":"registries","level":"view","items":null},
  {"resource":"git","level":"view","items":null},
  {"resource":"dns","level":"view","items":null},
  {"resource":"credentials","level":"view","items":null},
  {"resource":"certificates","level":"view","items":null},
  {"resource":"firewall","level":"view","items":null},
  {"resource":"settings","level":"view","items":null},
  {"resource":"audit","level":"view","items":null}
]'),
('Deploy', 'Creates, configures and deploys apps, and reads what they run against.', '[
  {"resource":"projects","level":"view","secrets":true,"items":null},
  {"resource":"apps","level":"manage","secrets":true,"items":null},
  {"resource":"domains","level":"view","items":null},
  {"resource":"databases","level":"view","items":null},
  {"resource":"storage","level":"view","items":null},
  {"resource":"servers","level":"view","items":null},
  {"resource":"templates","level":"view","items":null},
  {"resource":"registry","level":"view","items":null}
]');

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
ALTER TABLE api_keys DROP COLUMN access_role_id;
ALTER TABLE users DROP COLUMN access_role_id;
DROP TABLE access_roles;
