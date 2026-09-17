# Shells

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../../AGENTS.md).

`internal/shell` opens an interactive terminal in two places: inside an
app's running container, and on one of the instance's machines as root.
`internal/platform/terminal` is the wire both use. The dashboard reaches
them from an app's **Shell** tab and `/servers/<name>/shell`; the CLI from
`cubeship app shell` and `cubeship server shell`.

## One WebSocket is one session

`GET /api/apps/{project}/{env}/{name}/shell` and `GET
/api/nodes/{name}/shell` upgrade, and the session is the connection: it
starts when the socket opens and ends when it closes. There is no ticket
to mint first and no session row — a shell is a process somebody is
typing into, and the connection is exactly as long as that.

**The protocol is two kinds of frame.** A binary frame is terminal bytes,
unchanged, in either direction. A text frame is a JSON message about the
session: `resize` from the client, and `ready`, `exit` and `error` from
the daemon — plus `auth`, once, for the one session that asks for a
password. Keeping them in separate frame types rather than escaping one
inside the other means nothing a program prints is ever read as an
instruction, which is the bug every in-band terminal protocol has had.

**Refusals are frames, not status codes.** The upgrade is accepted as soon
as the caller is known and allowed to try, and "no permission", "no
container", "that image has no shell" come back as an `error` frame. A
browser's WebSocket reports a failed handshake as nothing but "error",
without the status or the body, so a refusal made before the upgrade is a
refusal nobody can read. Two things stay HTTP: nobody signed in (401,
from the same middleware as every other route) and a browser page from
another origin (403, below).

**These routes are `HandleInternal`.** The OpenAPI document describes
requests and their responses; a connection held open for as long as
somebody types is neither, and a document entry for one would describe
nothing a client could use.

`coder/websocket` is the library: small, maintained, context-aware, and
it honours `Unwrap` on a `ResponseWriter` — the audit middleware wraps
every authenticated route's writer, and a library that insisted on the
top-level writer being an `http.Hijacker` could not upgrade behind it.

## The origin is checked by hand

`httpx.SameOrigin` lets every GET through, because a GET changes nothing
— and a WebSocket is opened with a GET. **An app deployed on this
instance is same-site with the dashboard**, so its pages carry the
session cookie, and without a check of its own any of them could open a
shell as whoever visited. So a cookie-authenticated upgrade must carry a
same-origin `Sec-Fetch-Site`, or an `Origin` naming this host; neither is
refused. A request with an API key has no cookie to borrow and is not
checked.

## Who may

- **An app's shell is its own grant.** `Grant.Shell` on `apps`, and it
  needs **manage** too. It is never implied by manage and secrets: a
  shell reads every secret the running process holds *and* changes
  whatever the container can, so it is more than either — and every role
  written before shells existed has both, so implying it would have handed
  a shell to every deployer on the day of the upgrade. The member default
  does not have it.
- **A machine's shell is an unrestricted admin's, and no grant reaches
  it.** A root prompt on the host is the whole instance, and a role that
  could grant it would be a role that grants everything.
- **The dashboard asks for the password again** before a root shell, in
  the first frame, never in the URL — a query string lands in every
  access log between the browser and here. A session cookie proves
  somebody signed in once; this proves they are still at the keyboard.
  An API key is a secret the CLI holds and is its own proof. The check is
  throttled like a sign-in, because it is one.

Every session is **two audit events**: `GET …/shell` when it opens (or
is refused), and `CLOSE …/shell` when it ends, with how long it lasted
and whether the program exited, the connection dropped or it sat idle.
`CLOSE` is not an HTTP method; it is the one action here that no request
made, and naming it after the route it belongs to is what lets the log
describe both halves in the same sentence. **Nothing typed is recorded.**
A shell is where somebody pastes a password, and the audit log has never
kept a body.

## How a shell runs

**In a container**: `docker exec` with a TTY, the container's own user
and environment plus `TERM`. `sh` decides between `bash` and itself
inside the container, since which one exists is only known there. The
image is asked for `/bin/sh` first — an exec of a missing program fails
only after the terminal is open, in the Engine's words — so a distroless
image is a sentence rather than a terminal that opens and closes.

**Closing the terminal is not the process ending.** The Engine closes the
exec's stdin when the connection goes, and an interactive shell reads
that as logout; a program running in the foreground does not read stdin
at all, and would go on for ever inside somebody's app with nobody able
to see it. So an exec still running two seconds after its terminal closed
is sent `SIGHUP` from the host by its host pid — what a closed terminal
has always sent — and `SIGKILL` two seconds after that. That needs the
host, which is why `ContainerShell` is on `hostexec.Runner`; `make dev`
has no way onto the host and leaves such a process running.

**On the machine**: the door `hostexec` already opens for the firewall, a
privileged container of the daemon's own image in the host's PID
namespace running `nsenter -t 1`, held open as a TTY for the session
instead of run once. HOME and PATH are set to a root login's, because
nsenter changes namespaces and not variables, and a login shell reads the
host's profile for the rest. Its stdin closes with the connection, so a
daemon that dies takes its shells with it; the ones a harder crash leaves
are labelled `cubeship.shell` and removed when the daemon starts.

## On a worker

**Nothing dials a worker**, and a shell does not change that. The
control plane holds the person's connection under a random single-use id,
tells the worker on its parked poll (`CommandShell`), and the worker
dials `GET /api/nodes/agent/shell/{id}` with its own credential and runs
the session there. The control plane then copies frames between the two
connections without reading them — except the ones heading to the person,
for how the session ended, which is what the audit log writes.

```
browser ──ws──▶ control plane ── CommandShell on the worker's poll
                     ▲                          │
                     └─────── ws from worker ◀──┘  worker: docker exec / nsenter
```

- **The worker connects first and opens the program second**, so what
  goes wrong opening it reaches the person as a sentence rather than as a
  session that never arrived.
- **An id is claimed once, by the machine it was sent to.** Without that,
  any worker's credential would be a way onto somebody else's terminal.
  An id nobody claims in fifteen seconds ends the session with "the
  server did not open the shell in time".
- **A machine too old to have shells is refused at once**, from the
  version it reports, rather than after those fifteen seconds. A version
  that is not a release — a development build — is assumed current.
  So is an offline machine: nothing can reach it, and saying so now is
  better than a spinner.

Every keystroke to a worker goes through the control plane. In one region
nobody notices; across an ocean somebody does, and that is the price of a
worker with no open port.

## Keeping a session honest

- **Idle is thirty minutes with no key and no output.** Both, because a
  `tail -f` nobody is typing into is not idle and a shell somebody walked
  away from is.
- **A ping every thirty seconds**, from the daemon, because a browser never
  sends one and every proxy between here and it has an idle timeout —
  Traefik's is three minutes.
- **A frame may be a megabyte.** A paste is one frame, and the library's
  default of 32 KiB cuts a pasted file in half with an error nobody can act
  on.
- **Updating the daemon ends every session.** The process holding the
  connections is the one being replaced; the terminal says the connection
  closed and offers to reconnect.

## The dashboard

`TerminalSession` is xterm.js, loaded through `next/dynamic` so the app
page nobody opens a shell from does not carry it, into a box whose height
is fixed before anything loads — see "No layout shift". Its colours are
the `--ansi-*` variables the log panel uses, on the same black, so a
program's red is the same red in both. The Shell tab is mounted only while
it is open: a shell behind a tab nobody is looking at is a process nobody
will close. It is offered only to who may open one, and the preview build
says there is no machine behind it rather than failing to connect.

## What it is not

Not over MCP: handing an agent a root prompt or a command runner inside
somebody's container is a decision about agents, not about terminals, and
it is not made here. No recording, no second window on one session, no
file transfer.
