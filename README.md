# Cubeship

Cubeship is a self-hosted PaaS for a single VPS: you `docker push` an
image to the box, and it deploys. A daemon (`cubeshipd`) runs on the
server and manages three things — an embedded container registry, a
Traefik reverse proxy that terminates TLS and issues Let's Encrypt
certificates, and one container per app. Pushing a new tag fires a
registry notification, the daemon starts a container from the new image,
waits for it to look healthy, and only then retires the old one, so a
bad deploy never takes down a working app. A separate CLI (`cubeship`)
talks to the daemon's HTTP API from your machine.

Apps live in an environment, inside a project, inside an organization —
see [Projects, environments and env
vars](#projects-environments-and-env-vars) — and people get their own API
keys with a role in each organization they belong to — see
[Organizations, users and roles](#organizations-users-and-roles).

This repository is the core deploy engine: no Git-based builds, no web
UI, no multi-node.

## Build

```sh
go build ./cmd/cubeshipd   # the daemon, runs on the VPS
go build ./cmd/cubeship    # the CLI, runs on your machine
```

## Configuration

The daemon is configured entirely through environment variables.

| Variable | Required | Default | Meaning |
| --- | --- | --- | --- |
| `CUBESHIP_DOMAIN` | yes | — | Base domain. The API is served at `api.<domain>` and the registry at `registry.<domain>`, both through Traefik over HTTPS. Both names must resolve to this host for certificates to issue. |
| `CUBESHIP_ACME_EMAIL` | yes | — | Contact address Let's Encrypt registers for certificate expiry notices. |
| `CUBESHIP_DATA_DIR` | no | `/var/lib/cubeship` | Everything persistent: the SQLite database, the daemon's tokens, the registry's config, credentials and image storage, and Traefik's `acme.json`. Back this up. |
| `CUBESHIP_TOKEN` | no | generated | The daemon's **system** token: the shared secret the registry signs its push notifications with. It is not anybody's API key, grants no access to the daemon API, and is not a registry login credential — see [Organizations, users and roles](#organizations-users-and-roles) for how registry push/pull auth actually works. When unset, the daemon generates one on first start and stores it in `$CUBESHIP_DATA_DIR/token` (mode 0600), reusing it on every later start. |

Neither secret is written to the log — the daemon logs a short
fingerprint and the file path instead. To read them:

```sh
sudo cat /var/lib/cubeship/token           # webhook secret only
sudo cat /var/lib/cubeship/admin-api-key   # the super-admin's API key
```

`admin-api-key` is generated the first time the daemon boots against an
empty database, together with the super-admin user it belongs to. That
is the credential `cubeship login` takes.

## Running as a systemd service

A unit file is provided at [`deploy/cubeshipd.service`](deploy/cubeshipd.service).

```sh
sudo install -m 0755 ./cubeshipd /usr/local/bin/cubeshipd
sudo install -m 0644 deploy/cubeshipd.service /etc/systemd/system/cubeshipd.service
sudo systemctl edit cubeshipd            # set CUBESHIP_DOMAIN / CUBESHIP_ACME_EMAIL
sudo systemctl enable --now cubeshipd
```

The daemon needs access to the Docker socket, so it runs as root (or as
a user in the `docker` group, which is equivalent to root on that host).

## Firewall

**Port 9000 must not be reachable from the public internet.** The daemon
binds `0.0.0.0:9000` because the registry container reaches its webhook
through `host.docker.internal`, but that port serves the API in
plaintext, bypassing Traefik's TLS. Expose only 80 and 443:

```sh
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw deny 9000/tcp
```

## Organizations, users and roles

Every app belongs to an organization, and everyone who talks to the
daemon does so with their own API key.

- **Super-admin** — the VPS operator. Created on first boot with the key
  in `$CUBESHIP_DATA_DIR/admin-api-key`. Can do anything in any
  organization, and is the only role that can create organizations.
- **`admin`** (per organization) — can add users to that organization,
  and do everything a member can.
- **`member`** (per organization) — can create, deploy, configure and
  read the logs of that organization's apps.

A user can belong to several organizations, with a different role in
each. Another organization's apps are invisible to them.

```sh
# super-admin: create an org and its first admin
cubeship org create "Acme Inc" --slug acme
cubeship user create alice --org acme --role admin
# prints alice's API key, once — she saves it with `cubeship login`

# an org admin adds someone to their own org
cubeship user create bob --org acme --role member

# adding a user who already exists puts them in this org too,
# keeping the API key they already have
cubeship user create alice --org globex --role member

cubeship org list                  # orgs you belong to (all of them, if super-admin)
cubeship user api-key rotate       # replace the key you're using right now
```

Slugs are what the registry paths are built from, so they must be
lowercase letters, digits and dashes (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`).

### Multiple API keys

You aren't limited to one key: hold as many independent, named keys as
you like — one your terminal uses day to day, another for an MCP client
(see [MCP](#mcp)), another for a script. Rotating or revoking one never
touches any other.

```sh
cubeship user api-key create mcp   # prints a new key, shown once
cubeship user api-key list         # id, name, last used, which one you're using now
cubeship user api-key revoke 3     # by id from the list above; refused if it's your last key
```

## MCP

The daemon serves an [MCP](https://modelcontextprotocol.io) server at
`https://api.<domain>/mcp` (streamable HTTP transport). Everything the
CLI can do — organizations, projects, environments, apps, deploys, env
vars, logs, even your own API keys — is available as an MCP tool, so an
agent (Claude Code, for instance) can operate Cubeship directly.

Authentication is the same bearer API key as the rest of the API: send
`Authorization: Bearer <key>`. Give the MCP client its own key rather
than reusing your terminal's:

```sh
cubeship user api-key create mcp
```

Point your MCP client at `https://api.<domain>/mcp` with that key as
its bearer token — consult the client's own docs for exactly where a
custom header goes (for `claude mcp add`, `--header "Authorization: Bearer <key>"`).
Every tool call is authorized exactly like the equivalent HTTP request:
a member can create and deploy apps in their orgs, only an org admin can
create projects or environments or set project/environment env vars,
and only a super-admin can create organizations.

A few tools are worth calling out:

- `get_app_logs` returns the last 200 lines by default (`tail` overrides
  this), not the entire history — logs can be long, and an agent almost
  always wants the recent, relevant part.
- `rotate_my_api_key` replaces the key the MCP session is using *right
  now*. That session's next call fails with the old key — expected, but
  worth knowing before calling it on a whim.
- Registry push/pull auth (`docker login`, `docker push`) is not
  reachable through MCP: it runs on your own machine's Docker client,
  which the daemon has no way to drive remotely. `cubeship registry
  login` still covers that.

## Projects, environments and env vars

Every app lives inside an environment, which lives inside a project,
which belongs to an organization:

```
organization → project → environment → app
```

A project is created with one environment, `production`, which can
never be deleted. Create more environments (`staging`, `preview`, ...)
as needed — each is an independent namespace for its apps, with its own
domains and its own containers.

Environment variables can be set at three levels, and each app inherits
the levels above it:

- **project** — shared by every environment (and every app) in the project
- **environment** — shared by every app in that one environment
- **app** — that app only

A key set at a lower level overrides the same key set above it: an app's
own value wins over its environment's, which wins over its project's.

```sh
# super-admin or org admin: create a project (comes with "production")
cubeship project create "Web" --org acme --slug web
cubeship environment create "Staging" --org acme --project web --slug staging

# shared across the whole project, every environment, every app in it
cubeship project env set web --org acme DATABASE_URL=postgres://shared

# shared across every app in just this one environment
cubeship environment env set production --org acme --project web LOG_LEVEL=info

cubeship environment list --org acme --project web
cubeship environment delete staging --org acme --project web   # refused if it has apps, or if it's "production"
```

## Deploying an app

On your machine, once per daemon:

```sh
cubeship login https://api.example.com <your-api-key>
cubeship registry login
```

Registry push/pull is per-user, not a shared credential: `cubeship
registry login` looks up your own username and logs `docker` in with
your saved API key as the password. The registry only grants access to
the organizations you're actually a member of — a valid login for one
org's admin still gets rejected pushing to another org's namespace.
Super-admins can push anywhere.

Then per app:

```sh
cubeship app create myapp --domain myapp.example.com --org acme --project web
# --env defaults to "production"; prints the push path, e.g. registry.example.com/acme/myapp

docker build -t registry.example.com/acme/myapp:latest .
docker push registry.example.com/acme/myapp:latest   # this deploys

cubeship app logs myapp
cubeship app env set myapp KEY=value                 # this app only
cubeship app deploy myapp --tag v2                   # manual redeploy
```

App containers are expected to listen on port **8080**.

## Upgrading the daemon

`cubeshipd` leaves an already-running `cubeship-registry` or
`cubeship-traefik` container alone, and starts it if it exists but is
stopped. That means a release which changes how those containers are
configured needs the old container removed once, by hand, so the daemon
recreates it:

```sh
sudo systemctl stop cubeshipd
docker rm -f cubeship-registry
sudo systemctl start cubeshipd
```

Pushed images survive this — they live in
`$CUBESHIP_DATA_DIR/registry-data` on the host, not inside the
container.

### Upgrading from a release without organizations

The first start on an existing database migrates it: apps gain an owning
organization, and every app already there is adopted into one created
for them with the slug `default`. Nothing is redeployed and no image
path changes — an existing app keeps the image reference it was created
with, so pushes to it keep working. New apps in that organization get
the org-prefixed path (`registry.<domain>/default/<app>`).

Two things change for you on that first start:

- The API no longer accepts `CUBESHIP_TOKEN` as a bearer token. Log in
  again with the super-admin key from
  `$CUBESHIP_DATA_DIR/admin-api-key`, which that same start creates.
- `cubeship app create` now needs `--org`; use `--org default` for the
  organization your existing apps were adopted into, or create a new one.

### Upgrading from a release without projects and environments

The first start on an existing database migrates it: every app not
already in a project is adopted into one created for its organization
with the slug `default`, in that project's `production` environment.
Nothing is redeployed and no image path changes. New apps need
`--project` (see [Projects, environments and env
vars](#projects-environments-and-env-vars)); use `--project default` for
the project your existing apps were adopted into, or create a new one.

### Upgrading from a release with shared htpasswd registry auth

If you're upgrading from a release where `cubeship registry login`
took `--password <daemon-token>` and every push used one shared
`cubeship` account, the registry itself needs to switch from that
htpasswd credential to per-user token auth (see [Deploying an
app](#deploying-an-app)). The registry container has to be recreated
for its new config to take effect — the same `docker rm -f
cubeship-registry` step already covered under [Upgrading the
daemon](#upgrading-the-daemon) — and every user needs to run `cubeship
registry login` again (no more `--password` flag; it now uses your own
saved API key automatically).

## Tests

```sh
go test ./...                                # unit tests, no Docker needed
go test -tags integration ./test/integration # needs a Linux Docker daemon
```

The integration test brings up a real daemon, registry and Traefik,
pushes a fixture image and asserts the app answers over HTTPS. Its final
hop needs Linux: on Docker Desktop, `--network host` binds inside the
Docker VM rather than on your machine, so nothing listens on :443 there.
