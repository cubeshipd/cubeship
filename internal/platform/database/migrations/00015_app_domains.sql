-- +goose Up

-- An app is served at any number of names, and each one names a port.
--
-- Both halves of that changed at once, and they change together. An app
-- used to have one `domain` and every container was assumed to listen on
-- 8080 — so an image exposing something else could not be served at all,
-- and an image exposing several could only ever have one of them
-- reached.
--
-- The pair is the unit because the question "which port?" has no answer
-- per app once there is more than one name: api.example.com and
-- admin.example.com on one image are two ports on one container.
--
-- host is unique across the instance, not per app. Traefik routes by
-- host and nothing else; two apps claiming one name would give it two
-- answers, and which it picked would be a detail of label ordering.
CREATE TABLE app_domains (
    id      BIGSERIAL PRIMARY KEY,
    app_id  BIGINT  NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    host    TEXT    NOT NULL,
    -- 0 means "read it from the image". An image says what it listens
    -- on and that is where the answer already is; a number here is an
    -- operator overruling it, which is what an image exposing several
    -- ports — or none — needs.
    port    INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX app_domains_host ON app_domains (lower(host));
CREATE INDEX app_domains_app ON app_domains (app_id);

-- Whatever each app was serving at keeps serving at it.
INSERT INTO app_domains (app_id, host)
SELECT id, domain FROM apps WHERE domain <> '';

ALTER TABLE apps DROP COLUMN domain;

-- +goose Down
ALTER TABLE apps ADD COLUMN domain TEXT NOT NULL DEFAULT '';
-- Only one survives: the column holds one name, and an app may have
-- grown several. The oldest is the one it had before this migration.
UPDATE apps SET domain = COALESCE((
    SELECT host FROM app_domains d WHERE d.app_id = apps.id ORDER BY d.id LIMIT 1
), '');
DROP TABLE app_domains;
