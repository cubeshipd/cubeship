-- +goose Up

-- The path Traefik asks an app for to decide whether the container
-- behind a name is worth sending traffic to.
--
-- Empty is no check at all, and it is the default for every app that
-- exists — which is the only safe default there is. A health check
-- needs a path, the path is something only the app's author knows, and
-- a wrong one does not degrade anything: it marks every replica down at
-- once and turns a working name into a 503. So this is opted into, per
-- app, by somebody who knows what the app answers.
ALTER TABLE apps ADD COLUMN health_path TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE apps DROP COLUMN health_path;
