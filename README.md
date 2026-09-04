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
| `CUBESHIP_DATA_DIR` | no | `/var/lib/cubeship` | Everything persistent: the SQLite database, the API token, the registry's config, credentials and image storage, and Traefik's `acme.json`. Back this up. |
| `CUBESHIP_TOKEN` | no | generated | The API bearer token. When unset, the daemon generates one on first start and stores it in `$CUBESHIP_DATA_DIR/token` (mode 0600), reusing it on every later start. Set it explicitly only if you want to manage the secret yourself. |

The token is never written to the log — the daemon logs a short
fingerprint and the file path instead. To read it:

```sh
sudo cat /var/lib/cubeship/token
```

The same token is the registry password (username `cubeship`), which is
what `cubeship registry login` uses.

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

## Deploying an app

On your machine, once per daemon:

```sh
cubeship login https://api.example.com <token>
cubeship registry login
```

Then per app:

```sh
cubeship app create myapp --domain myapp.example.com
# prints the push path, e.g. registry.example.com/myapp

docker build -t registry.example.com/myapp:latest .
docker push registry.example.com/myapp:latest      # this deploys

cubeship app logs myapp
cubeship app env set myapp KEY=value
cubeship app deploy myapp --tag v2                 # manual redeploy
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

## Tests

```sh
go test ./...                                # unit tests, no Docker needed
go test -tags integration ./test/integration # needs a Linux Docker daemon
```

The integration test brings up a real daemon, registry and Traefik,
pushes a fixture image and asserts the app answers over HTTPS. Its final
hop needs Linux: on Docker Desktop, `--network host` binds inside the
Docker VM rather than on your machine, so nothing listens on :443 there.
