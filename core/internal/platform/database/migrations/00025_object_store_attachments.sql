-- +goose Up

-- One app wired to one bucket.
--
-- The bucket is on the attachment rather than on the store, because a
-- store holds many and an app wants one: S3_BUCKET has to have a value.
-- It is also why one app may be attached to the same store twice — a
-- bucket for uploads and one for backups is an ordinary shape, and the
-- prefix is what keeps their variables apart.
CREATE TABLE object_store_attachments (
    id              BIGSERIAL PRIMARY KEY,
    object_store_id BIGINT NOT NULL REFERENCES object_stores (id) ON DELETE CASCADE,
    app_id          BIGINT NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    -- Which bucket in that store this app is pointed at.
    bucket          TEXT NOT NULL,
    -- What the injected variables are named under. Empty for the usual
    -- case, which gives S3_ENDPOINT and its parts.
    prefix          TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The same app pointed at the same bucket twice would be one set of
-- variables written twice, and which one won would be a detail of map
-- iteration.
CREATE UNIQUE INDEX object_store_attachments_pair
    ON object_store_attachments (object_store_id, app_id, bucket);

-- Two attachments on one app that name the same variables.
--
-- Just the prefix, unlike the datastores' index, which also carries the
-- engine's stem: there every engine writes a different middle word, so
-- a Redis and a Postgres do not collide at the same prefix. Every
-- attachment here writes the same six names, so the prefix is the whole
-- of the namespace — and a column that would always hold 'S3' is a
-- column that says nothing.
CREATE UNIQUE INDEX object_store_attachments_app_vars
    ON object_store_attachments (app_id, prefix);

CREATE INDEX object_store_attachments_app ON object_store_attachments (app_id);

-- +goose Down
DROP TABLE object_store_attachments;
