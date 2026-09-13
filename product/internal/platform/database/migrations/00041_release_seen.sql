-- +goose Up

-- Which release each person has already been shown the notes for.
--
-- **Per person, not per instance.** "What changed" is a thing somebody
-- reads once; two admins on one box should each see it once rather than
-- whichever of them opened the dashboard first taking the notice away
-- from the other.
--
-- Its own table rather than a column on `users`, because it belongs to
-- the module that knows what a release is. A column there would make
-- `user` — which sits at the bottom and knows about no other module —
-- carry a fact about the changelog.
CREATE TABLE release_seen (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE release_seen;
