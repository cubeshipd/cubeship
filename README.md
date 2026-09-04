# Cubeship

A self-hosted PaaS for a single VPS: you `docker push` an image to the
box, and it deploys. A daemon (`cubeshipd`) runs on the server and
manages an embedded container registry, a Traefik reverse proxy that
terminates TLS with Let's Encrypt certificates, and one container per
app. Pushing a tag fires a registry notification; the daemon starts a
container from the new image, waits for it to look healthy, and only then
retires the old one, so a bad deploy never takes down a working app. A
CLI (`cubeship`) talks to the daemon's API from your machine.

Apps live in an environment, inside a project, inside an organization.
Everyone gets their own API key and a role per organization.

An app gets its image one of four ways: pushed to Cubeship's own
registry, where the push *is* the deploy; pulled from a registry
Cubeship does not run (Docker Hub, GitHub, DigitalOcean, ECR); built
here from a Dockerfile in a Git repository; or built from a repository
with no Dockerfile at all, worked out from the code. Only the first
deploys on its own — nothing tells Cubeship about a push or a commit
somewhere else. The rest need no domain and no certificate, so they work
the minute you have installed.

This is the core deploy engine: no Git-based builds, no web UI, no
multi-node.

## Install

On the server, as root:

```sh
curl -sSL https://cubeship.dev/install.sh | sh
```

It installs Docker if the box hasn't got it, verifies and installs the
daemon, registers it with systemd, and tells you where to open it.
Running it again upgrades in place. It is [one
file](install.sh) — read it first if you'd rather not pipe a script into
a shell, which is a reasonable thing to prefer.

Nothing has to be configured for it to start. The domain and the Let's
Encrypt contact address are set afterwards, from the dashboard — see
[Configuring the instance](#configuring-the-instance).

### Building it yourself

```sh
make build          # bin/cubeship and bin/cubeshipd for this machine
make release        # dist/<version>/, what install.sh serves
make help           # everything else
```

Both build the dashboard first, so they need Node. `go build` on its own
still works — you get a daemon that serves the API and says the dashboard
is missing. Point the installer at your own build with
`CUBESHIP_BASE_URL`.

What the environment still holds is defaulted in
[`internal/platform/config`](internal/platform/config/config.go) — most
importantly `CUBESHIP_DATA_DIR` (default `/var/lib/cubeship`), which
holds the database, the images and Traefik's `acme.json`. **Back it up.**
The daemon needs the Docker socket, so it runs as root.

State lives in Postgres. By default the daemon runs it for you, as a
`cubeship-postgres` container bound to loopback with its data under the
data dir — nothing to install. Set `CUBESHIP_DATABASE_URL` to point at an
existing server instead, and the daemon connects without managing it.

**Port 3000 is plaintext.** The daemon binds it on all interfaces so the
registry container can reach the webhook, and it serves the dashboard and
the API there too — bypassing Traefik's TLS. A fresh box has no domain
and no certificate, so this is the only way in and it has to be
reachable; a password and a session cookie cross it in the clear.

Once a domain is set, everything is reachable over HTTPS at
`api.<domain>` and 3000 has no remaining use from outside. Close it then,
and open only 80 and 443.

## Claiming the instance

Open `http://<ip>:3000` and create the account. A fresh instance has no
account and no way to add one from outside, so this first page creates a
super-admin, an organization and a project, signs you in, and closes
setup for good — every account after it is added from inside.

**Until you do this, whoever reaches that port first owns the instance.**
Claim it as soon as the daemon starts; it says so in its log while the
window is open.

You are signed in with a session cookie. For `cubeship login` and
`docker login`, issue yourself an API key under Account.

## Configuring the instance

Until a domain is set there is no registry to push to, and until a
contact address is set there are no certificates, so apps are served over
plain HTTP. Both are under **Instance** in the dashboard, or:

```sh
curl -X PUT https://api.example.com/api/settings \
  -H "Authorization: Bearer $KEY" \
  -d '{"domain":"example.com","acme_email":"admin@example.com"}'
```

Both `api.<domain>` and `registry.<domain>` must resolve to this host for
certificates to issue. Applying this replaces the affected containers,
which costs a few seconds of downtime for them; apps already running keep
the routing they were deployed with, so **redeploy them to serve over
HTTPS**.

## Deploy an app

```sh
cubeship login https://api.example.com <api-key>
cubeship registry login          # logs docker in as you, with your own key

cubeship org create "Acme" --slug acme
cubeship project create "Web" --org acme --slug web   # comes with a "production" environment
cubeship app create myapp --domain myapp.example.com --org acme --project web
# prints the push path: registry.example.com/acme/web/production/myapp

docker build -t registry.example.com/acme/web/production/myapp:latest .
docker push registry.example.com/acme/web/production/myapp:latest   # this deploys
```

An app is named by its reference — `<org>/<project>/<environment>/<app>`,
which is also its registry path. Three parts means `production`:

```sh
cubeship app list
cubeship app get acme/web/myapp
cubeship app logs acme/web/staging/myapp
cubeship app deploy acme/web/myapp --tag v2   # waits, but the deploy is the daemon's
cubeship app deployments acme/web/myapp       # how recent deploys went
```

Names only have to be unique within their environment, so the same app
can run in `production` and `staging` at once.

App containers must listen on port **8080**.

Environment variables can be set on a project, an environment or a single
app, and an app inherits all three — its own value winning, then its
environment's, then its project's:

```sh
cubeship app env set acme/web/myapp DATABASE_URL=postgres://...  # adds; leaves the rest alone
cubeship app env list acme/web/myapp        # every value, and which level set it
cubeship app env unset acme/web/myapp OLD_FLAG
```

`cubeship --help` covers the rest: users and roles, extra environments,
logs, manual redeploys, additional API keys.

## Signing in

The API takes two credentials. A key is what the CLI and MCP clients
use; a session is what a browser uses.

```sh
curl -X POST https://api.example.com/auth/login \
  -c cookies.txt -d '{"username":"admin","password":"..."}'
```

An account created by an organization admin has a key but no password
until it sets one, and cannot sign in before then:

```sh
curl -X PUT https://api.example.com/users/me/password \
  -H "Authorization: Bearer $KEY" -d '{"new_password":"..."}'
```

Changing a password ends every other session the account holds.

## API reference

The daemon serves a browsable reference of every endpoint at
`https://api.<domain>/docs`, rendered from the OpenAPI document at
`/openapi.json`. Both are unauthenticated — they describe the shape of the
API, never any data — so block them at the proxy if you'd rather not
advertise what runs here.

## MCP

The daemon serves an [MCP](https://modelcontextprotocol.io) endpoint at
`https://api.<domain>/mcp` over streamable HTTP. Everything the CLI can do
is available there as a tool, authorized exactly like the equivalent HTTP
request. Give the agent a key of its own:

```sh
cubeship user api-key create mcp
```

Point the client at the endpoint with that key as its bearer token — for
Claude Code, `claude mcp add --transport http cubeship https://api.example.com/mcp --header "Authorization: Bearer <key>"`.
The one thing MCP can't reach is registry push/pull auth: `docker login`
drives the Docker client on your own machine, which the daemon has no way
to touch.

## Develop

```sh
make check              # gofmt, go vet, unit tests under -race
make test-integration   # brings up a real daemon, registry and Traefik; needs Linux
```

The unit tests need a Postgres; `make test` starts one in a container
(`make db-up`, port 5433) and gives each test its own schema in it.

See [AGENTS.md](AGENTS.md) for the conventions, and
[docs/upgrading.md](docs/upgrading.md) when moving an existing install
onto a newer release.
