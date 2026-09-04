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

Apps belong to organizations, and people get their own API keys with a
role in each organization they belong to — see
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
| `CUBESHIP_TOKEN` | no | generated | The daemon's **system** token: the registry password (username `cubeship`) and the shared secret the registry signs its push notifications with. It is not anybody's API key and grants no access to the daemon API. When unset, the daemon generates one on first start and stores it in `$CUBESHIP_DATA_DIR/token` (mode 0600), reusing it on every later start. |

Neither secret is written to the log — the daemon logs a short
fingerprint and the file path instead. To read them:

```sh
sudo cat /var/lib/cubeship/token           # registry password / webhook secret
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
cubeship user api-key rotate       # revoke your key, get a new one
```

Slugs are what the registry paths are built from, so they must be
lowercase letters, digits and dashes (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`).

## Deploying an app

On your machine, once per daemon:

```sh
cubeship login https://api.example.com <your-api-key>
cubeship registry login --password <daemon-token>
```

The registry credential is still instance-wide — one account
(`cubeship`) whose password is the daemon's system token from
`$CUBESHIP_DATA_DIR/token` — so anyone who pushes images needs that
token as well as their own API key. It grants no access to the daemon
API. Per-organization registry authorization is the next piece of work;
until it lands, treat the daemon token as a shared push credential.
With no `--password`, `cubeship registry login` sends your saved API
key, which only works if that happens to be the daemon token.

Then per app:

```sh
cubeship app create myapp --domain myapp.example.com --org acme
# prints the push path, e.g. registry.example.com/acme/myapp

docker build -t registry.example.com/acme/myapp:latest .
docker push registry.example.com/acme/myapp:latest   # this deploys

cubeship app logs myapp
cubeship app env set myapp KEY=value
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

`cubeship registry login` is unchanged as long as you pass the daemon
token as the password (see above).

## Tests

```sh
go test ./...                                # unit tests, no Docker needed
go test -tags integration ./test/integration # needs a Linux Docker daemon
```

The integration test brings up a real daemon, registry and Traefik,
pushes a fixture image and asserts the app answers over HTTPS. Its final
hop needs Linux: on Docker Desktop, `--network host` binds inside the
Docker VM rather than on your machine, so nothing listens on :443 there.
