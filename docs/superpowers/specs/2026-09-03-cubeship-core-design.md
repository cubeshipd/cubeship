# Cubeship — Core Design (Sub-project 1)

## Context

Cubeship is a self-hosted PaaS (alternative to Dokploy/Coolify) that the
user runs on their own VPS. Initial goal is personal use, with an intent
to expand later (multi-tenant, web UI). This document specs the first
sub-project: the core deploy engine. Later sub-projects (not specced
yet): building images from a Git repo, managed databases, web UI.

Differentiator from Dokploy/Coolify: Cubeship has a built-in container
registry, and a `docker push` to it is the deploy trigger — no Git
webhook required for the core loop.

## Goals

- Deploy a containerized app to the VPS by pushing an image to Cubeship's
  own registry.
- Automatic HTTPS on a custom domain per app, zero manual proxy config.
- New image pushed to a tracked repository triggers automatic,
  zero-downtime redeploy.
- Both CLI (local, scriptable) and a documented HTTP API (for future
  Git-build integration and UI) are first-class.

## Non-goals (this sub-project)

- Building images from source/Git (sub-project 2).
- Managed databases (sub-project 3).
- Web UI (sub-project 4).
- Multi-tenant / multi-user auth (future, post-personal-use validation).

## Architecture

A single Go daemon runs on the VPS as a systemd service, with access to
the Docker socket (`docker/docker/client`). It manages three kinds of
containers:

- The registry (`distribution/distribution`, official OCI registry
  image) — configured with a notification webhook pointed back at the
  daemon.
- Traefik — reverse proxy + automatic Let's Encrypt certificates.
- The user's app containers.

"Built-in registry" means the daemon owns and configures this container
transparently as part of install/operation — not that the registry's Go
code is compiled into the daemon binary.

The CLI is a separate Go binary that runs on the user's machine and
talks to the daemon over HTTPS. The daemon's own API is exposed through
Traefik on its own subdomain (e.g. `api.<domain>`), authenticated by a
bearer token generated at install time.

## Components

- **Daemon**: HTTP API + SQLite state (apps, domains, tracked image
  reference, deploy history) + a reconciliation pass on startup that
  compares SQLite state against actual Docker state (recovers from a
  crash mid-deploy).
- **Registry**: stock `distribution/distribution` container, configured
  with a notification webhook to the daemon.
- **Traefik**: routing is driven entirely by Docker labels set by the
  daemon on app containers (`traefik.enable`, `Host(...)` rule, cert
  resolver). The daemon never writes Traefik config files directly.
- **CLI**: thin HTTP client. Commands: `app create`, `app deploy`
  (manual redeploy), `app logs`, `app env set`, `registry login` (wraps
  `docker login` against the embedded registry so `docker push` works).

## Data flow — deploying an app

1. `cubeship app create myapp --domain myapp.example.com` — daemon
   registers the app in SQLite, returns the image path in the internal
   registry (e.g. `registry.example.com/myapp`).
2. User builds locally and runs
   `docker push registry.example.com/myapp:latest`.
3. Registry accepts the push and fires its configured notification
   webhook to the daemon's `/hooks/registry` endpoint.
4. Daemon matches the pushed repository to a tracked app, starts a new
   container from the new image, waits for it to pass a health check,
   then swaps traffic to it (old container is stopped only after the
   new one is healthy — zero downtime).
5. Traefik picks up the new container via its labels automatically; the
   TLS certificate (issued on first deploy) is reused.

## Error handling

- A push to a repository with no tracked app: notification is logged
  and ignored, no error surfaced to the registry.
- New container fails to start or fails its health check: deploy is
  marked failed in history, the previously running (healthy) container
  is left untouched — a bad deploy never takes down a healthy app.
- Certificate issuance failure (e.g. DNS not yet pointed at the VPS):
  does not block the deploy; the app runs but is not reachable over
  HTTPS until DNS/cert resolve. Logged as a warning.
- Daemon restart mid-deploy: startup reconciliation detects the
  inconsistency between SQLite and actual Docker state and resolves to
  the last known-good container.

## Testing

- Unit tests for daemon logic (state transitions, reconciliation) with
  the Docker/registry clients mocked.
- Integration test: daemon + registry + Traefik brought up together
  (Docker Compose or testcontainers), a test image is pushed, and the
  test asserts the app becomes reachable over HTTPS with the expected
  response.
- CLI integration tests run against a local daemon instance.

## Decisions carried from brainstorming

- Language: Go, for ecosystem fit with Docker/OCI/Traefik (reference
  Docker client, easier path to embedding the registry as a library
  later if ever needed) over the user's initial Rust preference.
- Architecture: daemon + API (not SSH-only CLI), so Git-build
  integration and a future web UI plug into the same API without a
  rewrite.
- Registry: wrap the standard `distribution/distribution` image rather
  than implement the OCI protocol from scratch.
