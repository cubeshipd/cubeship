-- +goose Up

-- A GitHub repository carrying the cubeship-template topic. The id is
-- GitHub's own, so a rename or a transfer is an update, not a second row.
CREATE TABLE repositories (
    id                BIGINT PRIMARY KEY,
    node_id           TEXT NOT NULL UNIQUE,
    owner             TEXT NOT NULL,
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    url               TEXT NOT NULL,
    owner_avatar_url  TEXT NOT NULL DEFAULT '',
    stars             INTEGER NOT NULL DEFAULT 0,
    topics            TEXT[] NOT NULL DEFAULT '{}',
    -- NULL is listed. Otherwise why not: blocked, gone, untagged.
    hidden            TEXT,
    -- The newest accepted release; NULL until one is.
    latest_release_id BIGINT,
    first_seen_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    checked_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX repositories_path ON repositories (lower(owner), lower(name));

-- One release, read at the commit its tag pointed to. A tag moved to
-- another commit is another row, so nothing already indexed changes
-- under anybody.
CREATE TABLE releases (
    id            BIGSERIAL PRIMARY KEY,
    repository_id BIGINT NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    tag           TEXT NOT NULL,
    commit_sha    TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL,
    published_at  TIMESTAMPTZ NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('accepted', 'rejected')),
    -- Every diagnostic: the reasons for a rejection, the advice on an acceptance.
    problems      JSONB NOT NULL DEFAULT '[]',
    manifest      JSONB,
    source        TEXT,
    readme        TEXT,
    icon_key      TEXT,
    indexed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, tag, commit_sha)
);

CREATE INDEX releases_repository ON releases (repository_id, published_at DESC);

-- Owners ("someone") or repositories ("someone/cubeship-x-template")
-- the catalog refuses to list. Written by hand.
CREATE TABLE blocklist (
    subject    TEXT PRIMARY KEY,
    reason     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE blocklist;
DROP TABLE releases;
DROP TABLE repositories;
