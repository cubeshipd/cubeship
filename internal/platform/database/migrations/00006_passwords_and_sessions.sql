-- +goose Up
-- +goose StatementBegin

-- Until now the only way in was an API key, which suits a CLI and suits
-- nothing else: a browser needs an account to log into. The column is
-- nullable because an account can exist without one — the users an
-- organization admin creates get an API key immediately and a password
-- whenever they set it.
ALTER TABLE users ADD COLUMN password_hash TEXT;

-- Sessions are rows rather than signed cookies so they can be revoked:
-- logging out, and changing a password, have to end sessions that are
-- already out there.
--
-- Only the hash of the token is stored, the same way API keys are. A
-- database someone gets a copy of does not hand them live sessions.
CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	expires_at TIMESTAMPTZ NOT NULL,
	last_used_at TIMESTAMPTZ
);

-- Ending every session a user holds is what a password change does, and
-- it is the only query that does not go by token.
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- Expired rows are deleted on a schedule rather than read and rejected,
-- so the index that finds them matters.
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE sessions;
ALTER TABLE users DROP COLUMN password_hash;

-- +goose StatementEnd
