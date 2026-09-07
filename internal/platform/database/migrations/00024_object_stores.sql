-- +goose Up

-- One table for both kinds of object storage, because they are the same
-- thing reached two ways: an S3 endpoint, a login, and buckets inside
-- it. `kind` says whether this instance runs the server or merely holds
-- the keys to somebody else's, and that is the only difference the code
-- ever branches on.
--
-- Splitting them into two tables would have made every read a union and
-- every screen a choice, for two rows that differ in which columns are
-- filled in.
CREATE TABLE object_stores (
    id          BIGSERIAL PRIMARY KEY,
    -- The name, unique across the instance. For a managed store it is
    -- also the container's, which is the host apps resolve, so it is
    -- permanent either way.
    slug        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',

    -- 'managed' — a MinIO this instance runs. 'external' — an endpoint
    -- somewhere else.
    kind        TEXT NOT NULL,
    -- Which S3 this is: 'minio' for a managed one, and for an external
    -- one whichever provider decides how its endpoint is spelled.
    provider    TEXT NOT NULL,

    -- Where it answers, host and optional port, with no scheme: the
    -- scheme is `secure` below. Empty for a managed store, whose
    -- endpoint is derived from its own container name.
    endpoint    TEXT NOT NULL DEFAULT '',
    region      TEXT NOT NULL DEFAULT '',
    secure      BOOLEAN NOT NULL DEFAULT TRUE,
    -- Whether the bucket goes in the path rather than in the hostname.
    -- AWS wants virtual-host style; MinIO and most of the compatible
    -- ones want path style, and a wrong answer here is a DNS name that
    -- does not resolve.
    path_style  BOOLEAN NOT NULL DEFAULT FALSE,
    -- The one bucket this store is, when its credential reaches exactly
    -- one and listing them is refused — an R2 token scoped to a bucket,
    -- an IAM policy with no ListAllMyBuckets. Empty means "ask the
    -- endpoint what is there".
    bucket      TEXT NOT NULL DEFAULT '',

    -- The account an external store authenticates as. NULL for a
    -- managed one, whose keys are generated here and are not a secret
    -- anybody reuses. RESTRICT, like every other credential reference:
    -- a store whose login vanished is a store that cannot answer, and
    -- the way that surfaces is a page that fails minutes later.
    credential_id BIGINT REFERENCES credentials (id) ON DELETE RESTRICT,

    -- What a managed store runs, and the keys it was initialized with.
    -- The version is permanent for the same reason a database's is: the
    -- data directory belongs to the server that wrote it.
    version      TEXT NOT NULL DEFAULT '',
    access_key   TEXT NOT NULL DEFAULT '',
    secret_key   TEXT NOT NULL DEFAULT '',
    -- The host port it also answers on from outside this instance, or 0
    -- for reachable only by its neighbours — the default.
    exposed_port INTEGER NOT NULL DEFAULT 0,

    container_id TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT '',
    error        TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX object_stores_slug ON object_stores (slug);

-- A published port is the host's, so two things cannot share one. Only
-- the exposed ones are indexed: 0 is "not exposed" and every store that
-- is not would otherwise collide with every other.
--
-- This only keeps two object stores apart. What keeps one off a
-- datastore's port is that the two pick from different ranges — see
-- PortRangeStart — because a unique index cannot reach into another
-- module's table and neither module should be reading the other's.
CREATE UNIQUE INDEX object_stores_exposed_port
    ON object_stores (exposed_port) WHERE exposed_port <> 0;

CREATE INDEX object_stores_credential ON object_stores (credential_id);

-- +goose Down
DROP TABLE object_stores;
