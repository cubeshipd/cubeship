-- +goose Up
-- What an account says about the person behind it.
--
-- **The username stays what it is named by**, and these do not replace
-- it: it is what a session, an API key and a `docker login` are all
-- written against, and it is the segment in `/users/{username}`. What
-- these add is the part a username cannot carry — somebody's actual
-- name, an address to reach them at, and a face small enough to tell
-- two accounts apart in a sidebar.
--
-- Empty rather than null, the way every other optional text column
-- here is: every read gets a string and no caller has to think about
-- it.
--
-- `avatar` holds a name from a fixed list the daemon serves — see
-- user.Avatars — not a path and not a URL. The value ends up in an
-- <img src> on every screen, and a column that could hold either would
-- be a column somebody could put anything in.
ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN avatar TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN display_name;
ALTER TABLE users DROP COLUMN email;
ALTER TABLE users DROP COLUMN avatar;
