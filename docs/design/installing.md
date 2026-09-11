# Installing, and what the instance is configured with

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

## Installing

`install.sh` is the product's front door: it installs Docker if needed,
pulls the images and runs the daemon. **The release is two images** —
the daemon and the dashboard, at the same version — and nothing else has
to be hosted anywhere. An upgrade is still a pull.

**What it installs is an exact release, not `latest`.** With no
`--version` it asks GitHub which release is newest and pins that number.
That tag moves, so a box installed today and the same command run
tomorrow would be two different builds with no way to tell from the
outside which is which — and re-running an install is what somebody does
when something has gone wrong, which is the worst moment to change two
variables at once. It costs one HTTPS call on a machine that is about to
pull two images anyway, and **being unable to ask stops the install**
rather than falling back to `latest`, which would be doing the thing
this avoids, quietly.

`--version 0.2.0-rc.1` is the whole of installing a prerelease. A
version somebody named is left exactly alone.

The daemon starts the dashboard's container, so it has to know which
image: `CUBESHIP_WEB_IMAGE`. It is *told* rather than deriving it from
its own reference, because deriving means string surgery on a registry
path an operator is free to change, and getting it wrong is an instance
whose dashboard silently never starts. The daemon's image bakes in the
matching published version as the default; `install.sh` overrides it
with `--local`, where neither image is published.

`install.sh --control-plane <url> --token <token>` installs the same
image as a **worker** instead of as an instance. It pulls one image
rather than two, publishes no port, is told no domain, and waits for the
agent's own line in the log rather than polling a health endpoint there
is none of. Half of that pair is refused before the machine is touched:
an address with no credential is a box that dials forever and is always
refused, and a credential with nothing to dial is a box that does
nothing at all.

`uninstall.sh` is its counterpart, and the default is **not** the
destructive one: it removes the containers and leaves the data
directory, because someone removing the software is not thereby asking
to lose their database — installing again brings the same instance back.
`--purge` is the other thing. It lists what goes before it asks, and the
answer is the word "delete": a second button is no obstacle to a
misclick. With no terminal to ask on — which is what piping into a shell
means — it refuses rather than proceeding on silence, unless `--yes`
says otherwise.

`make test-uninstall` runs it on a Linux with Docker stubbed, including
the confirmation under a real pseudo-terminal. A destructive script that
is wrong is the worst kind.

`--local` builds both images from the checkout the script is in instead
of pulling, which is how you run unpublished code on a box: push, pull
on the server, install. The dashboard is built first and the daemon
last, because the daemon is what starts everything and a half-built pair
is better discovered before anything is replaced. It refuses when there
is no checkout beside it — piped from curl there is nothing to build,
and saying so beats a build that fails on a missing Dockerfile.

**The daemon is a container**, a sibling of Postgres, the registry,
Traefik, BuildKit, the dashboard and every app on the `cubeship`
network. Each finds the others by container name.

**The Engine is not one of the things an address follows.** It is the
host's daemon whatever Cubeship is, and it is on no user-defined
network — so an image reference beginning `cubeship-registry:5000` is
one it looks up through the *host's* resolver and never finds. Container
names live in Docker's embedded DNS, which only containers on that
network can ask. `bootstrap.LocalRegistryAddress` is where the **daemon**
reaches the registry's API and follows `InContainer`;
`bootstrap.PullRegistryAddress` is where the **Engine** pulls and is
loopback always, because the registry's port is published on the host
either way.

They were one value, and the Engine got the container's name. Every
deploy of an app on this instance's own registry failed with `lookup
cubeship-registry on 127.0.0.53:53: server misbehaving` — which reads as
a broken registry and is a name that was never going to resolve where it
was sent. It survived because the other way an app gets an image never
pulls: a build is loaded straight into the Engine (`Image.Local`), so an
instance that only builds never meets it. The pull address is also the
key `SetRegistryTokenSigner` is registered under, so the two are one
string: a token minted for one host is not attached to a pull from
another.

**`config.InContainer` is what decides every address**, and it is set in
the image rather than by whoever runs it. A daemon on the host is still
supported — that is what `make dev` runs — and reaches the same things
over loopback, with containers reaching back through
`host.docker.internal`. `bootstrap.PostgresDSN`, `LocalRegistryAddress`,
`DaemonAddress` and `FrontendAddress` are the four places that branch, and
`TestAddressesFollowWhereTheDaemonRuns` pins both modes: getting this
wrong is not a compile error and not a failure anywhere else, it is a
daemon that starts, looks healthy and cannot reach its own database.

**The machine's own `/proc` is mounted at `/host/proc`, read-only**, and
it is what makes the instance's network figures possible. `/proc/stat`
and `/proc/meminfo` are not namespaced — a container reading its own
gets the machine's, which is why `top` in one shows the host's memory —
but `/proc/net` **is**, so the interface counters a container reads are
its own veth. Through the mount, PID 1's entry is init's, which is in
the machine's network namespace by definition. It grants nothing the
daemon does not already hold: it has the Docker socket, which is root on
this box by another name. Without it, `internal/machine` reports the
network missing with that sentence rather than charting the daemon's own
traffic as the instance's.

**The data directory must be mounted at the same path inside and out.**
The daemon hands paths to the Engine when it creates its siblings, and
the Engine resolves them on the *host*. A different path inside would
make every one of those binds point at a directory that does not exist.
This is the one thing about running the daemon as a container that is
easy to get wrong and silent when wrong.

Traefik is no longer on the host's network namespace. It took it for one
reason — reaching a daemon at `127.0.0.1` — and it costs more than it
buys, not least that host networking does not work at all on Docker
Desktop, where the Engine runs in a VM.

`make test-install` runs the installer end to end in a Debian container
with Docker replaced by a recording stub. It sources the script minus its
last line — `main "$@"` — so the script itself carries no hook for the
test.

## Instance settings

The domain and the Let's Encrypt contact address (optional: an account
opens without one, so TLS follows the domain alone) are rows in `settings`,
not environment variables: Cubeship is installed with one command,
reached by IP, and configured from there. `config.Load` therefore
requires nothing, and `config.SeedSettings` carries the old environment
variables into the table once, for an install upgrading from the release
where they were mandatory.

Two things follow from a setting rather than being captured at startup,
because an operator changes them without restarting:

- **The registry host.** An app's push path is derived from its
  reference, never stored, so an app created before a domain existed gets
  a correct one the moment there is one.
- **Whether TLS is possible.** `traefik.Labels` takes it, and a container
  keeps the labels it was created with — an app deployed before
  certificates were possible stays on HTTP until it is redeployed.

Writing a setting re-runs `applyInfrastructure` in `cmd/cubeshipd`, which
is what brings the registry up when a domain appears. It works because
`bootstrap.Ensure` replaces a container whose configuration changed.

### What this instance's records point at

`public_ip` is the address Cubeship writes into a DNS record, and
`Service.PublicIP` is the four answers to it, best first:

1. **What the operator typed.** They can see the machine. It is the only
   answer that is not filtered — an instance behind a split-horizon
   resolver may want one nothing here would guess.
2. **The address the dashboard was opened at**, when the `Host` header
   is an IP literal. On a fresh install that is free and exactly right:
   the dashboard is reached at `http://<ip>:3000` before there is a
   domain, so it is by construction an address that reaches this host.
   Once there is a domain it is a name and this stops answering — which
   is the case that produced the bug below.
3. **The machine's own**, from `ip route get` run in the host's
   namespaces, through the same door the firewall uses. Cached for
   `HostAddressTTL`, because asking costs a container and a machine's
   address changes about never.
4. **The daemon's own interface**, which only ever answers on a daemon
   that is a host process — `make dev`.

**A private address is never an answer.** `Routable` refuses the bridge
range, RFC1918, loopback, link-local and carrier-grade NAT, and it is
applied to every answer but the first — including whatever a
`HostAddress` hands back, because that is the last place between a
detected value and somebody's zone.

That rule is the whole point of this section. The daemon runs as a
container, so before it existed the fallback read the *bridge's* address
and offered `172.18.0.2` as this instance's own. Written into a zone
that is not a guess that fails to help: it replaces whatever was
resolving at that name, and the domain goes dark. Empty is the honest
answer, and **every caller has to treat it as one** — the app's network
form disables its button and says why rather than adding a name it
cannot make resolve.
