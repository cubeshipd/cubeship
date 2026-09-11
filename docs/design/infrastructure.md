# Infrastructure containers

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

`bootstrap.Ensure` fingerprints the `ContainerOpts` it is given into a
`cubeship.config-hash` label. A container whose label still matches is
left alone or started; one whose settings changed is replaced, because
Docker cannot alter an existing container's image, binds, ports or
environment.

That is only safe because everything those containers must keep is in a
host bind mount. Anything you add to them has to keep that true —
persistent state inside a container's writable layer will be silently
destroyed the next time its configuration changes.

**Traefik's version is pinned, and the pin is load-bearing.** Its Docker
provider asks the Engine for a fixed API version, and up to v3.5 that
version is 1.24 — which Docker Engine 28 and later refuse outright.
Refused, not degraded: the provider retries forever and Traefik sees
**no container at all**, so every router that comes from a container
label silently does not exist.

Nothing about that looks broken from the outside. Apps deploy,
containers run, and the *file* provider carries on — so the daemon's own
name still routes and still gets a certificate, while everything routed
by a label answers with Traefik's default self-signed certificate. It
reads as a certificate problem, and it is a proxy that cannot see
anything. `TraefikImage` is v3.6, which negotiates the version;
`DOCKER_API_VERSION` is not a way out, because the older provider
ignores it.

`internal/certificates` carries the provider's own error into
`traefik_says` for exactly this reason — it is the only place the real
cause is written down, and it says nothing about ACME.

Traefik redirects the whole `web` entrypoint to `websecure`, so plain
HTTP reaches every app and the API without per-router labels. It does not
interfere with certificates: ACME uses the TLS-ALPN challenge on :443,
never the HTTP challenge on :80. Changing that would break the redirect.
