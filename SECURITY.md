# Security

Cubeship runs on somebody's own server, holds the credentials to
everything that server deploys, and reaches the Docker socket. Getting
this wrong costs the person running it their machine, so a report is
welcome and worth the trouble of making.

## Contents

- [Reporting something](#reporting-something)
- [What is supported](#what-is-supported)
- [What is worth reporting](#what-is-worth-reporting)
- [What is not a vulnerability](#what-is-not-a-vulnerability)
- [Known limits, written down on purpose](#known-limits-written-down-on-purpose)
- [What happens after you report](#what-happens-after-you-report)

## Reporting something

**[Open a private advisory](https://github.com/cubeshipd/cubeship/security/advisories/new).**
It is GitHub's private vulnerability reporting — the report is visible to
nobody but you and the maintainer until there is a fix, and it becomes the
advisory that is published with one.

**Do not open a public issue for it.** Every instance of Cubeship is
somebody's VPS, and they upgrade by hand: the gap between a public report
and an operator getting around to `install.sh` is the window the report
opens.

What makes a report actionable, roughly in order of how much it helps:

1. **What an attacker ends up holding** — an admin's session, another
   app's environment, a shell on the host.
2. **Who they had to be to start** — nobody at all, a member, a deployed
   app, a worker's credential, somebody with a link.
3. **The requests.** `curl` is fine. So is a paragraph, if the shape is
   clear enough to follow.
4. **The version** — `cubeship version`, or **Settings** in the dashboard.

A proof of concept is welcome and never required. A report that is right
and vague is worth more than one that is precise and wrong.

**Please do not test against a machine that is not yours.** An instance
belongs to whoever installed it, and every app on it belongs to somebody
else again.

## What is supported

**The newest release, and nothing behind it.** There are no backport
branches: a fix is a normal release with a note in the
[changelog](CHANGELOG.md), and the instances that get it are the ones that
upgrade.

That is a real property of this project rather than an oversight — the
upgrade is one command, or a time of day the instance does it by itself,
and maintaining a second line would mean the fix landing later in both.

```bash
curl -fsSL https://raw.githubusercontent.com/cubeshipd/cubeship/master/install.sh | sh
```

A release candidate — anything with `-rc` in the version — is never what
you get by default and is not supported. If you are running one, the fix
for you is the next stable release.

## What is worth reporting

The things this daemon promises, in the order it would hurt to lose them:

- **Claiming an instance without the setup token.** The token is what
  makes taking over a fresh box require access to the host.
- **Reaching an authenticated endpoint without a credential** — or with
  somebody else's.
- **A session cookie being enough for a write.** Apps deployed here answer
  at sibling names under the same domain, so the cookie *is* sent
  cross-site; the same-origin check on state-changing requests is what
  stands in for a CSRF token, and a way around it is a way to act as
  whoever visits a page.
- **A member doing an admin's job** — building from source, writing a
  building app's environment, reading credentials, adding a user.
- **A deployed app reaching the daemon**, the instance's database, another
  app's environment, or the registry as anything but itself.
- **A worker's credential doing more than it should.** It may report in,
  be told what to run, and pull. Pushing, deleting, or reading another
  machine's work is not its to do.
- **A registry token outside its scope** — pushing to a repository it was
  not minted for, or deleting anything at all.
- **A secret coming back out.** Datastore passwords, stored credentials,
  the GitHub App's private key and webhook secret are write-only by
  design; a build log, an API response, an error message or the OpenAPI
  document handing one over is a bug.
- **A forged GitHub delivery** starting a deploy, or an installation being
  connected to an instance by somebody who does not administer it.
- **Escaping validation into something that is interpreted** — a slug, an
  app reference, a host or a health path reaching a registry path, a
  container name, a Traefik rule or a shell argument as anything but the
  value it was meant to be.

## What is not a vulnerability

Each of these is a deliberate property, documented where it lives. They
are listed so that a report about one gets an explanation instead of
silence — and if you think the reasoning is wrong, that is a conversation
worth having in an issue rather than an advisory.

- **The daemon holds the Docker socket, which is root on the host by
  another name.** Everything Cubeship does is create containers; it cannot
  do that without it.
- **`hostexec` runs a privileged container that enters PID 1's
  namespaces.** That is how the firewall rules get written. It adds no
  privilege over the socket above — anything that can create containers
  can create that one.
- **The BuildKit container is privileged.** Building an image means
  running one.
- **A build executes whatever is in the repository**, on the host. That is
  what a build is, and it is why building is an admin's job and not a
  member's.
- **An admin can take the machine.** An admin configures the instance,
  builds arbitrary source on it and reads its credentials. The boundary
  worth defending is member → admin, not admin → root.
- **`/healthz`, `/openapi.json` and `/docs` answer without a
  credential.** The reference fetches the document from a browser with
  nothing to offer, and none of the three says anything about what is on
  the instance.
- **An exposed database or object store has no TLS.** Publishing one is
  opt-in, the endpoint says so, and what protects it is the password and a
  firewall rule.
- **Stored secrets are stored, not hashed.** A datastore's password and a
  registry's login have to be given back to the thing they authenticate
  to. Passwords that a person types — the only ones that can be — go
  through Argon2id.

## Known limits, written down on purpose

Not bugs, and not secrets either. If one of these is what bit you, the
answer is here rather than in an advisory:

- **Docker publishes ports around UFW.** A published port is forwarded
  rather than delivered to the host, so `ufw allow` never sees it. That is
  the entire reason `internal/firewall` has a scope on every rule and
  refuses an `apps` rule until the host's `DOCKER-USER` chain has been
  adopted.
- **A firewall at the provider is a third layer this instance cannot
  see.** Hetzner, Contabo, DigitalOcean and AWS filter in front of the
  machine, and nothing here reads or writes that.
- **A mesh network created before encryption was asked for stays
  unencrypted.** Docker fixes the flag when the network is created and
  offers no way to change it. `cubeship server list` says so under the
  table, and the fix — removing the overlay — takes every container off
  the cluster's network until each is created again.
- **Everything arrives at the control plane.** It is the proxy for the
  whole instance, so it is where an attack on ingress lands and where the
  certificates are.

## What happens after you report

**You get a reply within 48 hours** — an acknowledgement that it has been
read and what it looks like from here, not a fix. That is a promise about
the one part that is always possible to keep: a report sitting unanswered
for a week is what makes people publish instead of writing next time.

After that:

- **Something real is fixed in the next release**, and the release is cut
  for it rather than waiting for whatever else was planned. How long that
  takes depends on what it is, and you will be told which release to
  expect it in rather than left guessing.
- **The advisory is published with the fix**, so an operator can tell
  whether the upgrade in front of them is one they need today.
- **"That is intended, and here is why"** is a legitimate outcome and not
  a brush-off. If the reasoning is wrong, say so — a few of the entries in
  the two lists above are there because somebody argued the point.

Credit goes in the advisory and in the release note under whatever name
you want, or none.
