-- +goose Up
ALTER TABLE bitbucket_connections ADD COLUMN api_token TEXT;
ALTER TABLE bitbucket_connections ALTER COLUMN consumer_id DROP NOT NULL;
ALTER TABLE bitbucket_connections ALTER COLUMN access_token DROP NOT NULL;
ALTER TABLE bitbucket_connections ALTER COLUMN refresh_token DROP NOT NULL;
ALTER TABLE bitbucket_connections ALTER COLUMN expires_at DROP NOT NULL;

-- +goose Down
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM bitbucket_connections WHERE api_token IS NOT NULL) THEN
    RAISE EXCEPTION 'remove Bitbucket API-token connections before rolling back';
  END IF;
END $$;
ALTER TABLE bitbucket_connections ALTER COLUMN consumer_id SET NOT NULL;
ALTER TABLE bitbucket_connections ALTER COLUMN access_token SET NOT NULL;
ALTER TABLE bitbucket_connections ALTER COLUMN refresh_token SET NOT NULL;
ALTER TABLE bitbucket_connections ALTER COLUMN expires_at SET NOT NULL;
ALTER TABLE bitbucket_connections DROP COLUMN api_token;
