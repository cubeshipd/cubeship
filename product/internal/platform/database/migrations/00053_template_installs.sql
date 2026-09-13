-- +goose Up
-- One install of a template from the catalog, and what it created.
--
-- `resources` is that list in the order things were created, and it is
-- the whole of what undoing an install needs: delete what is in it,
-- newest first, and touch nothing that existed before the install ran.
-- A project the template was installed into is not in it.
CREATE TABLE template_installs (
    id          BIGSERIAL PRIMARY KEY,
    owner       TEXT NOT NULL,
    repo        TEXT NOT NULL,
    release     TEXT NOT NULL,
    commit_sha  TEXT NOT NULL,
    project     TEXT NOT NULL,
    environment TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    step        TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    resources   JSONB NOT NULL DEFAULT '[]',
    created_by  BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX template_installs_recent ON template_installs (created_at DESC);

-- +goose Down
DROP TABLE template_installs;
