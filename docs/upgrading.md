# Upgrading an existing install

## From a release with no dashboard

The daemon now serves a dashboard at the root, and everything the API
answers moved under `/api`. If you call the API directly — a script, a
CI job, anything holding a bearer token — its URLs gain that prefix:
`https://api.example.com/api/apps`, not `.../apps`. `cubeship` and the
MCP endpoint are unchanged; the CLI knows, and `/mcp` did not move.

Six addresses stay at the root, because a person or another program has
them written down: `/healthz`, `/openapi.json`, `/docs`, `/mcp`, and the
registry container's `/v2/token` and `/hooks/registry`.

The document at `/openapi.json` now offers server URLs ending in `/api`,
so a client generated from it needs no edit beyond regenerating.

Building from source needs Node for the dashboard — `make build` and
`make daemon-linux` run it. A plain `go build` still produces a working
daemon; it serves the API and reports the dashboard as missing.

## From a release that bootstrapped a super-admin on first boot

Nothing to do on an install that already has accounts — bootstrap only
ever ran against an empty database, and this release simply stops it
from running at all. `$CUBESHIP_DATA_DIR/admin-api-key` is no longer
written or read; the key it holds still works, since it is a row in the
database like any other, but nothing recreates it if you delete the file.

A new install now starts with no account and claims itself through
`POST /setup`: the first request creates a super-admin, an organization
and a project, and closes setup permanently. The daemon logs a warning on
every start until that happens, because until it does, anyone who can
reach the port can claim the instance.

Two things follow from setup creating the account instead of bootstrap:

- The username is yours to choose. It was hardcoded to `admin`, which is
  also the `docker login` username — so a registry login on a new install
  uses whatever name you picked.
- The account is created with a **password**, and no API key. Setup signs
  you in with a session cookie; issue a key from `POST /users/me/api-keys`
  when you want one for the CLI or for `docker login`.

## From a release that required CUBESHIP_DOMAIN and CUBESHIP_ACME_EMAIL

Nothing to do, but worth knowing what changed. The daemon now starts
knowing neither: it is meant to be installed with one command, reached by
IP, and configured afterwards. Both values moved to the instance's
settings, editable through `PUT /settings`.

Your existing environment variables are read once, on the first start
after upgrading, and written into the settings — so an install that had
them keeps working unchanged. After that they are ignored: the settings
are the source of truth, and a value changed through the API is never
overwritten by the environment.

Two things behave differently while an instance has no domain or contact
address configured, which cannot happen to an upgraded install but is the
normal state of a new one:

- No domain means no registry container and no push path. Apps can still
  be created; `image` comes back empty until a domain exists.
- No contact address means no certificate resolver, so apps are served
  over plain HTTP and `:80` is not redirected. Setting one and redeploying
  an app moves it to HTTPS.

## From a release where deploying blocked the request

`POST /apps/.../deploy` used to run the deploy inline and answer 200 or
502 when it ended. It now answers **202** with a deployment id, and the
work runs detached — a client that hangs up no longer kills a deploy
halfway.

If you call the API directly, follow the deployment:
`GET /apps/<ref>/deployments/{id}?wait=true` holds the response open
until it finishes, and `GET /apps/<ref>/deployments` lists recent ones.
`cubeship app deploy` still waits and still prints the outcome, so
nothing changes at the command line except that Ctrl-C now stops the
watching rather than the deploy.

## From a release where app names were unique instance-wide

An app's name used to be unique across the whole Cubeship, which made
environments useless as namespaces: the same app could not live in both
`production` and `staging`, and two organizations could not both have an
`api`. A name is now unique only within its environment.

That changes two things you use directly:

- **An app is named by its reference**, `<org>/<project>/<environment>/<app>`
  — `cubeship app get acme/web/production/myapp`. Three parts means the
  `production` environment, so `acme/web/myapp` works too. The HTTP API
  moved from `/apps/{name}` to `/apps/{org}/{project}/{env}/{name}`, and
  the MCP tools take `app` instead of `name`.
- **The registry path gained the same scope**:
  `registry.<domain>/<org>/<project>/<environment>/<app>`. The first
  start rewrites the stored path for every existing app, so the push
  webhook keeps matching — but images you already pushed stay at the old
  path. Push once to the new one, which `cubeship app get` prints, and
  the deploy runs as usual.

Nothing is redeployed, and running containers are untouched.

## From a release that stored its data in SQLite

**There is no automatic migration, and the old data is not read.** The
daemon now keeps everything in Postgres; on the first start it brings up
a `cubeship-postgres` container, creates an empty schema in it, and
bootstraps a new super-admin. The old `cubeship.db` is left untouched
under the data dir and simply ignored.

For an instance with real data in it, that means recreating your
organizations, projects, users and apps by hand — there are no automated
steps to give you. Pushed images survive (they live in
`registry-data`, not in the database), but an app has to exist again
before a push to it deploys anything.

Two things change for you on that first start:

- Every previously issued API key is gone with the old database. The
  empty database makes the instance a fresh one, so it is claimed through
  `POST /setup` like a new install, and everyone else is added again.
- The data dir gains `postgres/` (the database) and `postgres-password`.
  Back up the whole directory, as before; `postgres/` is now the part
  that matters most.

To point the daemon at a Postgres you already run instead of the managed
container, set `CUBESHIP_DATABASE_URL` before starting it.

## Any upgrade

Nothing to do. `cubeshipd` records on each of its containers a
fingerprint of the settings it was created from, and replaces one whose
settings have changed — Docker cannot alter an existing container's
image, binds, ports or environment, so a new setting only takes effect
in a new container.

That costs a few seconds of downtime for whichever container changed, on
the start that changes it. Nothing is lost: everything those containers
must keep lives in a bind mount on the host — pushed images in
`registry-data`, certificates in `letsencrypt/acme.json`, the database in
`postgres/`.

The first start after upgrading to this behaviour replaces all three
once, since containers created before it carry no fingerprint. That is
the `docker rm -f` step earlier releases asked you to run by hand.

## From a release without organizations

The first start migrates the database: every app is adopted into an
organization created for it with the slug `default`. Nothing is
redeployed and no image path changes, so existing pushes keep working;
only apps created from then on get the org-prefixed path
(`registry.<domain>/default/<app>`).

Two things change for you:

- The API no longer accepts `CUBESHIP_TOKEN` as a bearer token. Log in
  again with the super-admin's own API key.
- `cubeship app create` now needs `--org`.

## From a release without projects and environments

The first start adopts every app that isn't in a project into one created
for its organization with the slug `default`, in that project's
`production` environment. Nothing is redeployed and no image path
changes. `cubeship app create` now needs `--project`.

## From a release with shared htpasswd registry auth

If `cubeship registry login` used to take `--password <daemon-token>` and
every push went through one shared `cubeship` account, the registry has
to switch from that htpasswd credential to per-user token auth. The
daemon recreates it on the next start (see [Any upgrade](#any-upgrade));
every user then runs `cubeship registry login` again — no `--password`
flag now, it uses their own saved API key.
