-- +goose Up
-- A template installed on this instance, and every change made to it.
--
-- `template_installs` is the installation: the release it is on, the
-- template's normalized manifest at that release, the answers that are
-- not secret, and what it created — each with the template key it came
-- from. The manifest and the keys are what an update compares a newer
-- release against; without them there is no telling which app is `web`.
-- A project the template was installed into is not in `resources`.
CREATE TABLE template_installs (
    id          BIGSERIAL PRIMARY KEY,
    owner       TEXT NOT NULL,
    repo        TEXT NOT NULL,
    release     TEXT NOT NULL,
    commit_sha  TEXT NOT NULL,
    project     TEXT NOT NULL,
    environment TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('installing', 'installed', 'failed', 'uninstalled')),
    manifest    JSONB NOT NULL DEFAULT 'null',
    answers     JSONB NOT NULL DEFAULT '{}',
    resources   JSONB NOT NULL DEFAULT '[]',
    created_by  BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX template_installs_recent ON template_installs (created_at DESC);

-- One install, update or uninstall of an installation.
--
-- `created` is what the run made, newest last, and `snapshot` is each
-- existing app as an update found it: between them they are the whole
-- of putting things back when a run fails. A run is its own row so a
-- failed update leaves the installation it failed on exactly as it was.
CREATE TABLE template_install_runs (
    id           BIGSERIAL PRIMARY KEY,
    install_id   BIGINT NOT NULL REFERENCES template_installs (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('install', 'update', 'uninstall')),
    from_release TEXT NOT NULL DEFAULT '',
    to_release   TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    step         TEXT NOT NULL DEFAULT '',
    error        TEXT NOT NULL DEFAULT '',
    created      JSONB NOT NULL DEFAULT '[]',
    snapshot     JSONB NOT NULL DEFAULT '[]',
    keep_data    BOOLEAN NOT NULL DEFAULT true,
    created_by   BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ
);

CREATE INDEX template_install_runs_install ON template_install_runs (install_id, id DESC);
CREATE INDEX template_install_runs_running ON template_install_runs (status) WHERE status = 'running';

-- +goose Down
DROP TABLE template_install_runs;
DROP TABLE template_installs;
