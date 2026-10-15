-- +goose Up
-- Small profile images belong to the identity and follow its backup/deletion lifecycle.
CREATE TABLE user_avatars (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    media_type TEXT NOT NULL CHECK (media_type IN ('image/png', 'image/jpeg', 'image/webp')),
    image BYTEA NOT NULL CHECK (octet_length(image) BETWEEN 1 AND 524288)
);
ALTER TABLE users ALTER COLUMN avatar SET DEFAULT '';

-- +goose Down
DROP TABLE user_avatars;
UPDATE users SET avatar = 'cyan' WHERE avatar = '' OR avatar LIKE 'upload:%';
ALTER TABLE users ALTER COLUMN avatar SET DEFAULT 'cyan';
