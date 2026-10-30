-- A caller-chosen name identifies repeated installations of one template.
-- Empty preserves records created before installation names existed.
-- +goose Up
ALTER TABLE template_installs ADD COLUMN installation_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE template_installs DROP COLUMN installation_name;
