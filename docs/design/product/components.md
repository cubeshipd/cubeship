# Cubeship components

`internal/components` is the read-only view of Cubeship's own services on each
machine. The Dashboard exposes it as **Platform / Components**, with the same
machine selector used by Overview.

The inventory is a fixed allowlist using bootstrap's actual container names:
Cubeship daemon, Dashboard, Traefik, instance Postgres, registry and BuildKit.
It does not include application containers, user databases or managed object
stores. A worker normally runs only the daemon in agent mode. Other known
Cubeship components are included on a worker only if their containers exist.

Docker supplies state, health check result, image, start time and restart count.
A missing container is shown as not installed, rather than presumed failed:
BuildKit runs on demand, and the database, daemon or Dashboard can run externally.
Inspection failures remain unavailable. Stopped containers can still expose logs.
No environment variables, mounts, credentials or arbitrary Docker inventory are
returned.

## Authorization and remote reads

Both endpoints require an unrestricted administrator:

- `GET /nodes/{name}/components`
- `GET /nodes/{name}/components/{component}/logs?tail=200`

Application log permissions do not grant access to internal component logs,
which can contain operational details and secrets. The component identifier is
checked against the fixed list on both control plane and worker. There is no
arbitrary container-name parameter, shell execution or lifecycle action.

The control plane reads its local Docker engine directly. Remote inventory and
logs use `components` and `component-logs` commands over the existing node reverse
channel. The worker still dials out; nothing dials its Docker socket or opens a
port. Offline machines are refused immediately; in-flight reads are bounded by
the command timeout. Older agents answer with the existing upgrade explanation.

Logs combine stdout and stderr, decoding Docker framing unless the container uses
a TTY. Requests accept 1–5000 tail lines and cap output at 2 MiB. Log responses
are plain text with no-store and nosniff headers. The viewer supports filtering,
copying, download, refresh and optional Follow. Queries are cancelled when their
machine or component changes; Follow never starts overlapping requests and cached
log text is discarded when the viewer unmounts.

The page has no restart, stop or delete controls. Operational changes continue
through the existing install, update and configuration flows.
