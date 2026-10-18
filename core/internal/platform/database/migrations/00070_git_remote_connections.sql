-- +goose Up
CREATE TABLE git_remote_connections (
  id BIGSERIAL PRIMARY KEY,
  provider TEXT NOT NULL CHECK (provider IN ('gitlab', 'gitea')),
  base_url TEXT NOT NULL,
  account_id BIGINT NOT NULL,
  account TEXT NOT NULL,
  api_token TEXT NOT NULL,
  webhook_secret TEXT NOT NULL,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (provider, base_url, account_id)
);

-- +goose Down
DROP TABLE git_remote_connections;
