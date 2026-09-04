-- +goose Up
-- +goose StatementBegin

-- The domain and the ACME email used to be required environment
-- variables, which forced an operator to know both before the daemon
-- would start at all. They are now configuration the instance carries and
-- the dashboard edits, so a fresh install can boot, be reached by IP, and
-- be configured afterwards.
CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- An app's registry path was frozen into this column when the app was
-- created, from a domain that had to already exist. It is derived from
-- the app's reference now, so an app created before a domain is
-- configured gets a correct push path the moment one is.
ALTER TABLE apps DROP COLUMN image;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- The old column was NOT NULL UNIQUE; it cannot be restored with correct
-- values without knowing the registry host, so this restores the shape
-- and leaves the daemon to recompute on the next deploy.
ALTER TABLE apps ADD COLUMN image TEXT NOT NULL DEFAULT '';
DROP TABLE settings;

-- +goose StatementEnd
