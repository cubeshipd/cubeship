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

This is the core deploy engine: no Git-based builds, no web UI, no
multi-node.

## Install

```sh
make build          # bin/cubeship and bin/cubeshipd for this machine
make daemon-linux   # cross-compile the daemon for the VPS
make help           # everything else
```

On the server, install the binary and the unit file at
[`deploy/cubeshipd.service`](deploy/cubeshipd.service):

```sh
sudo install -m 0755 bin/cubeshipd-linux-amd64 /usr/local/bin/cubeshipd
sudo install -m 0644 deploy/cubeshipd.service /etc/systemd/system/
sudo systemctl edit cubeshipd     # set CUBESHIP_DOMAIN and CUBESHIP_ACME_EMAIL
sudo systemctl enable --now cubeshipd
```

`CUBESHIP_DOMAIN` and `CUBESHIP_ACME_EMAIL` are required; both
`api.<domain>` and `registry.<domain>` must resolve to this host for
certificates to issue. The rest is optional and defaulted in
[`internal/platform/config`](internal/platform/config/config.go) — most importantly
`CUBESHIP_DATA_DIR` (default `/var/lib/cubeship`), which holds the
database, the images and Traefik's `acme.json`. **Back it up.** The
daemon needs the Docker socket, so it runs as root.

State lives in Postgres. By default the daemon runs it for you, as a
`cubeship-postgres` container bound to loopback with its data under the
data dir — nothing to install. Set `CUBESHIP_DATABASE_URL` to point at an
existing server instead, and the daemon connects without managing it.

**Port 9000 must not be reachable from the public internet.** The daemon
binds it on all interfaces so the registry container can reach the
webhook, but that port serves the API in plaintext, bypassing Traefik's
TLS. Open only 80 and 443.

The first boot against an empty database creates a super-admin and writes
its API key to `$CUBESHIP_DATA_DIR/admin-api-key`, mode 0600. That is the
credential `cubeship login` takes — nothing is printed to the log but a
fingerprint.

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
