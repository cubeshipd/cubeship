-- +goose Up
-- +goose StatementBegin

-- A deploy no longer runs inside the request that asked for it, so its
-- row is created when the deploy is accepted and finished when it ends.
-- That needs a third state, and the two that existed become consistent
-- with it.
UPDATE deployments SET status = 'succeeded' WHERE status = 'success';
ALTER TABLE deployments ALTER COLUMN status SET DEFAULT 'pending';

-- Deployments are read newest-first for one app, which is the only way
-- anything queries them.
CREATE INDEX deployments_app_id_created_at_idx ON deployments (app_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX deployments_app_id_created_at_idx;
ALTER TABLE deployments ALTER COLUMN status DROP DEFAULT;
UPDATE deployments SET status = 'success' WHERE status = 'succeeded';

-- +goose StatementEnd
