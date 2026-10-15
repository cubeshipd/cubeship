-- +goose Up
-- Everybody has a face.
--
-- It was optional for one release, and "none" was a real answer: the
-- picker offered it first and the sidebar drew the first two letters of
-- the username instead. That is two things the same row can be, and the
-- one it almost always was is the one nobody chose — an account arrived
-- with no face and stayed that way, so the feature's ordinary state was
-- its fallback.
--
-- So the default becomes a face rather than the absence of one. Cyan,
-- because it is the interface's own accent and the palette an account
-- already starts on: a new account looks like this instance rather than
-- like an account that has not been set up.
--
-- The backfill is what makes the rule true rather than merely declared.
-- A default only reaches rows made after it, and every account on every
-- instance running the release before this one has an empty string in
-- there.
UPDATE users SET avatar = 'cyan' WHERE avatar = '';
ALTER TABLE users ALTER COLUMN avatar SET DEFAULT 'cyan';

-- +goose Down
-- The default goes back and the rows do not. Somebody who chose a face
-- chose it; blanking every account to undo a default would be this
-- migration deleting what it did not write.
ALTER TABLE users ALTER COLUMN avatar SET DEFAULT '';
