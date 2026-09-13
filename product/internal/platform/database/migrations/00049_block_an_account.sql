-- +goose Up
-- An account that is shut out without being deleted.
--
-- Deleting was the only answer before this, and it is the wrong one for
-- everything except somebody leaving for good: it takes the account,
-- its keys and its sessions in one transaction, and there is no undoing
-- it — the username is free for anybody to claim again, and nothing
-- remembers what the account was. Somebody suspended for a week, or an
-- account being looked into, needed a door that closes and opens.
--
-- **A timestamp, not a boolean.** When it happened is the first thing
-- anybody asks afterwards, and a `blocked BOOLEAN` answers it with
-- nothing while costing exactly as much to store. NULL is not blocked,
-- which is every account that exists when this runs.
ALTER TABLE users ADD COLUMN blocked_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users DROP COLUMN blocked_at;
