# Credentials, and other people's registries

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

## Credentials

`internal/credential` holds the secrets this instance is wired to — an
AWS access key, a Cloudflare token, a registry password — and it exists
because the same secret kept being entered twice. An AWS key is the same
key whether Route 53 writes a record with it or ECR is pulled from with
it, and it used to live once under DNS providers and again under
registries: two rows, two rotations, and one of them forgotten.

**A credential is a label, an optional first half and a secret, and
nothing else.** It carries no provider. It did once, and the provider
was what said which API the daemon speaks with it — which made a
credential a thing you create *per provider*: most API tokens can only
be read at the moment they are issued, so a secret filed under the one
job it may ever do has to be issued a second time for the second job.
The point of storing a secret centrally is exactly the opposite.

**Which API is spoken belongs to the use, and every use keeps a row.**
A registry names its provider (`generic`, `digitalocean`, `aws`) and the
credential it logs in with; a DNS provider names Route 53 or Cloudflare
and the credential it writes through. One credential may be named by any
number of them, which is the payoff: one AWS key, entered once, writing
records with one hand and pulling images with the other.

**A credential is a convenience, not a prerequisite.** This is the part
that is easy to get backwards, and was: every screen that uses one also
*makes* one. A registry is added with a login typed in place and the
credential is created from it — one request, one transaction, so a
secret is never left behind by a registry that turned out to be
unreachable — and it then appears under Credentials, ready to be picked
for the second registry or for DNS. `POST /registries` and `POST /dns`
therefore take either a `credential_id` or a `label`/`username`/
`password`, and refuse both at once, which has no obvious reading.

What it must not become is a gate in front of adding a registry, which
is the tail wagging the dog — nobody's first act on a fresh instance is
naming an account.

Rotating from a registry rotates **the credential**, and everything else
on it follows. A caller who wanted only that one registry to move wanted
a different credential, which is what `credential_id` is for; the screen
says which other things share it before the button is pressed.

The module knows nothing about DNS or registries. What it knows is
`Dependant`, an interface the modules that *use* credentials implement:

- `Resolve(ctx, caller, id)` is the one way to get a secret. It asks no
  question about what the secret is for, because that is not this
  module's to answer — whether a token works for a job is the job's to
  refuse.
- `UsesCredential` is each dependant's answer to "what would deleting
  this break", so `in_use_by` can say so in the listing and a delete
  that would strand something is refused with the names.

`server` is where the two halves meet: `creds.SetDependants(registries,
dnsProviders)`, at wiring time, the same seam `project.AppTeardown` uses
and for the same reason — the module that owns the rows is the only one
that can answer, and it sits above the one asking.

**A secret is stored as given and never returned.** A provider takes the
secret itself, so a hash could not be sent to one; an endpoint that
handed it back would turn every read of the list into a way out for it.
`PATCH` with no password leaves it alone, which is what makes renaming
a credential not a rotation.

Managing them is an **admin's** job, reads included: the list names what
secrets this instance holds and what stands on each, which is not
something a member needs and is exactly what somebody probing would
want.

There are no MCP tools here, deliberately — creating one means a
password passing through a model's context — and the same reasoning that
kept them off `extregistry`.

Two migrations tell the story. `00021_credentials.sql` moved the rows
in: `dns_providers` became credentials and every external registry got
one derived from its own login. `00023_credentials_are_generic.sql`
undoes the half of that which went too far — the registries get their
`provider` column back, `dns_providers` returns as a provider and a
credential id with no secret in it, and `credentials.provider` is
dropped. Both foreign keys are `ON DELETE RESTRICT`, because a
credential something stands on must not vanish underneath it.

## Pulling from someone else's registry

`internal/extregistry` says which registries Cubeship does not run,
which kind each is, and which credential logs in to each. The login
itself lives in `credentials` — one DigitalOcean or ECR account covers
every image on it, and rotating a password is one edit there rather than
one per row. One registry per host, or "which one does this pull use"
has no answer.

The **provider is the registry's own column**, not the credential's: a
credential is a secret, and the same DigitalOcean token may be reaching
two different things. A row is joined to its credential on every read,
so everything above this — the provider clients, the deploy path — still
sees one value with a provider and a login on it. None of them had to
learn where the login moved.

Matching is by host, and the two sides have to agree about spelling —
`NormalizeHost` reduces what someone types, `HostOf` reads what an image
reference carries, and both land on `index.docker.io` for a reference
with no registry in it at all.

**The host and the provider are fixed once a registry exists.**
Re-pointing one in place would silently send an app's pulls somewhere
else, and the host was derived from the provider; what can be changed is
which credential it authenticates as — a second AWS account, not a
second address — and the secret that credential holds.

The two are not the same edit and cannot be asked for together: one
moves this registry, the other moves every registry on that account.
Rotating also drops the cached ECR token minted from the key that just
changed, or pulls would go on working for hours on the old one and then
fail for no visible reason.

A missing credential is not an error. Public images need none, and
letting the registry be the one to refuse is what keeps a deploy that
would have worked from being blocked on a guess.

`ImageSource.Resolve` returns an `app.Image` — a reference and the
credentials for it — because that is one answer. Resolving the reference
is what determines which registry is involved, and so which login
applies.

There are no MCP tools for any of this, deliberately: creating a
credential means a registry password passing through a model's context.
