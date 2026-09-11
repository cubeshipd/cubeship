# Where an app answers, and what stands in front of it

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

## Where an app answers, and on which port

An app is served at any number of names, and **each name carries its own
port**: `app_domains` is `(host, port)`, not a domain column on the app.

The pair is the unit because "which port does this app listen on" stops
having one answer as soon as the app has more than one name. An image
can expose several, and `api.example.com` and `admin.example.com` on one
container are two of them.

That is also why Traefik gets **one router and one service per domain**
rather than a single `Host(a) || Host(b)` rule. A router has one
service, so every name behind one rule would reach the same port.

`host` is unique across the instance, not per app. Traefik routes by
host and nothing else; two apps claiming one name would give it two
answers, and which it picked would be a detail of label ordering.

**An app is offered a name under the instance's own domain.**
`SuggestedHostFor` builds `<app>.<environment>.<project>.<instance
domain>` and the app's response carries it as `suggested_host`. Nothing
assigns it — see above — but giving an app an address used to mean
owning a domain, pointing a record at this host, and waiting for it. A
default install's domain is an sslip.io address, and *every* name under
one of those resolves to the same host with nothing registered
anywhere, so under one the suggestion is a name that works the moment it
is added. `settings.ResolvesEveryName` is what knows the difference, and
`wildcard_domain` in the settings response is what tells the dashboard
whether to also write a record. Each name is still its own Let's Encrypt
certificate; a wildcard would need DNS-01, which an IP-embedding service
has no API for.

**Port 0 means "read it from the image"**, and is the normal answer.
`EXPOSE` ends up in an image's config, so `dockerx.ExposedPorts` reads
it where it already is — the Dockerfile is not always around, and never
is for an image someone else built. An image exposing nothing has no
answer and one exposing several has no *single* answer, so both fall
back to `DefaultPort`; a number on the domain is an operator overruling
that.

The image is inspected inside the deploy, after `Resolve`, because that
is the first moment it certainly exists: an app that builds has no image
until its first build. Nothing can detect a port when the app is
configured, which is why the field is always offered.

A container keeps the labels it was created with, so adding or removing
a name changes nothing until the app is redeployed.

## Where an app answers from *inside* the instance

Everything above is about the world reaching an app. An app reaching its
neighbour is a different question, and the public name is the **wrong
answer to it**: the request leaves the box for a record that points back
at the box, and a host that does not hairpin its own NAT answers
nothing at all. What that looks like from the calling app is the other
app being down — never a wrong address — which is why this is worth a
name of its own rather than a line in a FAQ.

`app.InternalHost` is that name: `cubeship-<project>-<environment>-<app>`,
the shape `cubeship-db-pg` and `cubeship-s3-media` already have, so
reaching a database, a bucket and an app is one thing to learn rather
than three. The app's response carries it as `internal_host`, always —
it is derived from the reference, so an app with no public name at all
still has one, and a worker with no domain is exactly the app most
likely to be called this way.

**It is a network alias, not the container's name**, and that is the
whole mechanism. A container is named for the deployment that created
it — `cubeship-web-production-api-1421` — because a machine has to be
able to answer "am I already running this deployment" from the name
alone (see `containerNameFor`). Anything that wrote *that* down would be
addressing a container that stops existing on the next deploy. The alias
is the stable half, attached at creation on the local bridge and on the
mesh both, so the address means the same thing from another machine.

Two things fall out of it for free, and neither is code here:

- **Every copy of the app holds it**, and Docker's embedded DNS answers
  with all of them — so a scaled-out app is spread over without this
  being a second load balancer beside Traefik's.
- **Nothing is in the path.** No proxy, so no TLS is terminated for it,
  the health check does not apply, and the port is the app's own rather
  than the one a name routes to. That is worth saying on the screen,
  because "it works publicly and not internally" and "it works
  internally and not publicly" have completely different causes.

The one edge is upgrade: a container picks the alias up when it is
created, so an app that has not deployed since the instance gained this
does not answer to it yet. The dashboard says so rather than detecting
it — detecting it means asking the Engine about every app on every
settings screen, and being told once is cheaper than a name that
quietly resolves to nothing.

## Certificates

`internal/certificates` reports what this instance holds and what it is
missing. It **issues nothing**: Traefik does that, through the ACME
resolver it is started with, and keeps the result in
`<data dir>/letsencrypt/acme.json` — the whole store, with no API in
front of it and no row about it anywhere here.

The daemon can read that file because the data directory is mounted at
the same path inside and out, which is the same reason everything else
about that directory works. It decodes each entry's PEM, takes the
leaf's own facts — names, issuer, validity, serial — and never touches
the private key sitting beside it.

What the store cannot answer is what the page is for. `reconcile` lines
the certificates up against every name this instance routes (an app's
domains, plus the instance's own two) and says which app each one
serves, which certificate serves nothing any more, and why a name that
should have one does not: no domain on the instance at all, a name added
after the app's last deploy — a container keeps the labels it was
created with — or Traefik knowing the name and not having got one yet.
That last case quotes the ACME error from `docker logs
cubeship-traefik`, because it appears nowhere else; a log line that stops
matching leaves the field empty and the report is still the report. The
complaints that name no host are carried too, in `traefik_says`: the
commonest failure on a default install is a rate limit, and Let's
Encrypt counts every name under `sslip.io` against one weekly allowance
shared with everyone else using it, because `sslip.io` is not on the
Public Suffix List.

**"Routed" is checked, not assumed.** The daemon's own name comes from
the dynamic file the daemon writes, so it is there whenever there is a
domain; the registry's comes from its container's labels, and a
container keeps the labels it was created with. One made before the
domain existed carries no router at all, so Traefik has never heard of
the name and `pending` would be a lie — the report inspects the
container and says `not_deployed` instead.

**It is read-only on purpose.** Renewing or deleting means editing a file
Traefik owns while it runs, which is only safe with the container
stopped — a few seconds of downtime for every app — and every re-issue
spends one of a weekly limit shared with everyone else using the same
registered domain. That is a decision to make deliberately, not a button
beside a table.

### Asking again

**Traefik asks a CA for a certificate when its configuration changes and
at no other moment.** It walks the routers on each configuration it is
handed and resolves what it has not got; between two of those there is
no timer and no API. So the first attempt for a name was, on a settled
instance, the only one it ever got — a record written a minute too late,
or an hour when Let's Encrypt's remote vantage points could not reach
the nameservers, left a name with no certificate for the life of the
instance. Nothing was trying, nothing said so, and the way out was
redeploying an app with nothing wrong with it.

`certificates.Retrier` is what asks again, every `RetryInterval`, for as
long as any name is `pending`. The other reasons are left alone on
purpose: no domain at all, an app not deployed since the name was added,
and a name on another machine are three things for a person to do, and
asking a CA would move none of them.

**The lever is a file beside the routes that nothing refers to.**
`traefik.RetryCertificates` writes and removes
`traefik-dynamic/retry.yml`, which holds one `serversTransport` no
service names — a genuine change to the document Traefik builds and no
change to anything served. The routes themselves could not be the lever:
rewriting them identically is a configuration Traefik skips, and
rewriting them in two steps is a moment with a router missing, which is
a name off the internet. Presence is the whole state, so nothing has to
be remembered across a restart — whichever way the file is, the other
way is a change.

Half an hour is a rate limit rather than a preference. Every attempt
asks for **every** name that is missing one, and Let's Encrypt allows
five failed validations per hostname per hour; two of those leaves room
for the deploys and the by-hand retries of whoever is fixing it. The
first tick is a whole interval away because Traefik resolves everything
it is missing when it starts, which is what an install, an upgrade or a
reboot already is.

This does not make the module read-write. Traefik still owns
`acme.json`; what is spent is attempts.

## The firewall

`internal/firewall` is the host's UFW: whether it is on, what it admits,
and one thing that is not UFW's at all. It **owns no rows** — the rules
live in UFW, where an operator's own `ufw` command looks for them, and a
second copy here would be a copy that drifts the first time somebody
types `ufw allow` over SSH. Same shape as `certificates`, which reads
Traefik's store.

**Docker publishes ports around UFW, and that is the whole design.** A
published port is DNAT'd and *forwarded* to a container rather than
delivered to the host, so it never passes the INPUT chain `ufw allow`
and `ufw deny` govern — and every port Cubeship opens is one of those:
Traefik's 80 and 443, an exposed datastore's, the daemon's own. A screen
wrapping `ufw status` would therefore show a firewall that is not in
front of anything you deployed, which is worse than showing nothing.

So a rule has a **scope**. `host` is `ufw allow`, traffic to the machine.
`apps` is `ufw route allow`, traffic forwarded to a container — and it
only means anything once `AdoptDocker` has appended a stanza to the
host's `/etc/ufw/after.rules` sending Docker's `DOCKER-USER` chain
through UFW's forward chain first. `DOCKER-USER` is the one seam Docker
leaves and never rewrites. Until that stanza is there, an `apps` rule is
**refused rather than written**, because a rule that governs nothing is
the exact lie this module exists to avoid.

**An `apps` rule is written for the port *inside* the container, not the
one somebody typed.** The forward chain is consulted after
nat/PREROUTING, so by the time UFW sees a packet sent to a published
15000 it is addressed to 5432 — and a rule naming 15000 matches nothing,
for ever, while reading as correct on every screen. `Spec.Inside` is the
translated port and `Status.Published` is where it comes from: Docker
reports both halves of every mapping, so the number is read off what is
running rather than guessed at.

It shipped wrong, and the reason it went unnoticed is worth keeping:
everything Cubeship publishes for itself uses one number inside and out
— Traefik's 80 and 443, the daemon's 3000 — and the only mappings that
translate are exactly the ones an operator exposes, a database on
15000-15999 listening on 5432 and a store on 16000-16999 listening on
9000. So the feature worked for every port the product opens and for no
port anybody opens themselves. A rule for a port nothing publishes is
written unchanged, which is what keeps a rule added ahead of the thing
it is for from becoming a rule for a number nobody chose.

**A firewall at the provider is a third layer, and it is not visible
here.** Contabo, Hetzner, DigitalOcean and AWS all filter in front of
the machine, and that layer does *not* have the Docker problem: it drops
a packet before it reaches the host's netfilter at all, so
`DOCKER-USER` never enters into it. An operator who has one is already
covered for published ports, and one who has not — or who has one and
does not know how to use it — is what this module is for. What follows
is that the screen must not claim to know what is *reachable*: it knows
what this machine is offering, and says so in those words.

**A firewall that is off still has rules**, and reading only `ufw
status` misses them: it answers "inactive" and stops, while what
somebody added sits in ufw's file waiting to be applied. That was a dead
end with no way out — adding the rule for SSH did nothing visible, so
the check below never saw it, so enabling stayed refused however many
times you added it. `ufw show added` is what answers while it is off. It
prints commands rather than a table, which is also how a rule is removed
then: there are no positions until the firewall is running.

Three refusals are the point of the module, and each is a thing that
silently costs somebody a machine:

- **Enabling with nothing admitting SSH** — which `Enable` prevents by
  *writing that rule itself*, not by refusing. Refusing was the first
  answer and it was the wrong shape: if a rule is compulsory, making
  somebody add it by hand is a mechanical step in front of a button, and
  one they can get wrong, for a rule the daemon already knows how to
  write. The port comes from the host's own `sshd -T`. The refusal
  survives for the single case where the guarantee cannot be kept — a
  host whose sshd did not say — because any rule written there would be
  a guess, and a wrong guess *is* the lockout. For the same reason the
  detected list is never filled in with 22: a guess is harmless while it
  only decides whether to refuse, and is a lockout once it decides which
  port to open.
- **An `apps` rule before adoption**, above.
- **Deleting the rule that admits SSH**, while the firewall is running.
  It is the same guarantee as the one above, from the other side:
  enabling writes that rule so the session survives, and letting the
  next click remove it would make the promise last exactly as long as
  nobody was curious. It is still removable on the machine, where the
  person doing it can watch what happens, and the refusal names the
  command. Every rule admitting a port sshd is on is protected rather
  than "the last one" — somebody moving SSH to another port adds the new
  rule, moves sshd, and the old one stops being protected on the next
  read, because what counts is where sshd is *now*.
- **Editing** is a delete and an add, because UFW has no edit — and the
  order is the safety: the new rule goes in **first**, at the old one's
  position, and the old one after. The reverse has a window where the
  rule is simply gone, and an add that then fails leaves a firewall
  missing a line nobody removed on purpose; this way the window holds a
  duplicate, which is harmless. The position is kept because order is
  meaning here — the first rule that matches decides, so a rule appended
  to the end may now be shadowed by one above it.
- **Deleting by a position that has moved.** UFW deletes by number and
  numbers shift; the caller sends the rule's own text and the daemon
  refuses if it no longer matches. Otherwise a stale screen deletes a
  different rule and the only sign is a port that stops answering.

**One request, one rule per source.** UFW takes a single source per rule
— there is no "from A or B" — so admitting a port from three addresses
is three rules. That is the tool's shape and not worth hiding, but it
should not make the *asking* three times either: `firewall.Request`
carries the list, every spec is checked before any is written, and an
empty entry means anywhere, which absorbs the rest. The screen offers
"anywhere", "this computer" and an address, because `your_ip` is on the
status — the address a person almost always wants is the one they would
otherwise go and look up, and get wrong by pasting a private one the
firewall never sees.

Adopting **asks which ports stay open**, ticked by default. The safe
default for a firewall is the state the machine is already in: somebody
who wants a port closed says so, and somebody who does not know yet does
not lose a service by pressing a button they were told to press. 80 and
443 cannot be unticked, because they are the page the button is on.

Adopting is the dangerous direction, and the **order is not cosmetic**:
the `ufw route allow` rules go in first, while they are inert, and the
stanza that starts denying goes in last. 80 and 443 are allowed whatever
the caller asked for — they are Traefik, which is every app and the
dashboard the button was pressed from. Everything else currently
published is offered, because `Status` reports it: on this kind of host
what is exposed is what containers publish, not what the host's own
services listen on.

There are **no MCP tools**, deliberately. The line is the one
`extregistry` draws by having none at all, for a different reason: an
agent that closes 443 takes the instance off the internet, and nothing
about that is worth automating.

### Reaching the host at all

The daemon is a container on a bridge network; `ufw` is a host program
editing the host's netfilter tables. `internal/platform/hostexec` is the
bridge: a throwaway container in the host's PID namespace, privileged,
running `nsenter -t 1 -m -u -i -n -p --` against PID 1. What runs is
then the host's own binary with the host's filesystem and network.

**It adds no privilege the daemon does not already hold.** The daemon
has `/var/run/docker.sock`, which is root on the host by another name —
anything that can create containers can create a privileged one. This is
that existing door, used deliberately in one place, rather than left for
a future module to reinvent worse.

The image is the daemon's own, read back from the Engine
(`bootstrap.OwnImage`) rather than configured: it is Alpine, busybox
carries `nsenter`, and it is on the box by definition — so there is no
third image in the release and nothing to pull the first time a rule is
written. It is off entirely when the daemon is a host process, which is
`make dev`, where it would be editing the developer's own firewall.

Nothing user-supplied is ever interpolated into a command line.
`Spec.Check` matches every field against a pattern — a port is digits or
a range, a source is an address, a comment is a short line of ordinary
characters — and `Spec.Args` builds argv rather than a string. The
stanza reaches the host through the **data directory**, not through an
argument: it is mounted at the same path inside and out, so the host
reads a file the daemon just wrote and several hundred bytes of iptables
syntax never pass a shell.
