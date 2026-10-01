-- +goose Up
CREATE TABLE bitbucket_connections (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 account_uuid TEXT NOT NULL UNIQUE,
 account_name TEXT NOT NULL,
 consumer_id TEXT NOT NULL,
 access_token TEXT NOT NULL,
 refresh_token TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE bitbucket_oauth_states (
 state_hash TEXT PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 consumer_id TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL
);
-- +goose Down
DROP TABLE bitbucket_oauth_states;
DROP TABLE bitbucket_connections;
