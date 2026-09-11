# Object storage

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

`internal/objectstore` is the buckets this instance can reach, and the
files inside them. There are two ways one gets here and **one module**:
a MinIO this instance runs, or an S3 endpoint somewhere else that it
holds the keys to. `Kind` is the only branch, and everything above the
connection — browsing, uploading, who may look — is one code path.

They belong together because they are the same thing from every
direction that matters: an endpoint, a login, and buckets inside it. Two
modules would have meant two of every screen for two rows that differ in
which columns are filled in.

**Why both exist** is the honest part. The convention for object storage
is backups, and a backup of this machine kept on this machine is not a
backup — so the common case is linking a bucket somewhere else. It is
not the only case: a MinIO on the box is where an app's uploads go
without an account anywhere, where a database dump lands before
something ships it off, and where you develop against S3 without paying
for S3. Which of those somebody is doing is not this module's to decide.

**It stores no files and proxies every byte.** A download is read from
the store and written to the response, an upload the other way; there is
no cache and no copy. A presigned URL would be faster and would be
wrong: a managed store's endpoint is a container name that resolves on
the Docker network and nowhere else, so the link would be one only the
daemon could follow.

### The client is minio-go, not awssig

`internal/platform/awssig` signs this daemon's other AWS calls and takes
the body as a `[]byte`, because the signature covers its hash. That is
right for an ECR token request and impossible for a file: it would mean
holding every upload in memory to sign it. Streaming means either
`UNSIGNED-PAYLOAD` or S3's chunked signature, and which one a provider
accepts is exactly the kind of difference that arrives as a refusal with
no explanation. Multipart upload, XML listings, path style against
virtual host and per-provider error codes are the rest of it.

`Client` is the interface the use cases see and `Connector` is how one
is opened, so everything worth testing here — who may look, which bucket
a request may reach, what a folder is — is tested with a map instead of
a container.

### What a folder is

**S3 has no directories.** A listing returns keys and common prefixes,
and a folder is the second of those: it exists while something is under
it and stops existing when the last thing goes. Three things follow, and
each of them is a decision somebody will otherwise re-litigate:

- Deleting a folder deletes **everything under it, at every depth**,
  because there is nothing else to delete. An empty prefix is refused
  rather than obeyed — emptying a whole bucket is what a missing query
  parameter looks like.
- An **empty folder is a zero-byte object** whose key ends in a slash,
  which is the convention every console uses. The listing hides that
  marker for the folder being listed, or every folder anybody made would
  contain a nameless file.
- A folder has **no size**. Working one out is a listing of everything
  under it, on every row of the table.

A key is not a path, so `../` means nothing to S3 and would simply
create an object called that. `CleanKey` refuses it anyway: this
instance's own listing is built out of prefixes, so a name that walks
out of the folder it was uploaded to is a file that appears somewhere
nobody put it.

### Where a linked store is

Four providers, and each is an endpoint template with **one variable in
it** — a region for S3 and Spaces, an account id for R2, and for
`generic` the address itself. Asking for the variable beats asking
somebody to retype `s3.eu-central-1.amazonaws.com` and get one character
wrong. R2's region is the literal string `auto`, which is not a default
anybody may override: a signature computed for anything else is refused.

The **host and the provider are fixed** once a store exists, the same
rule `extregistry` keeps for the same reason: re-pointing one in place
would silently send every app configured against it somewhere else. What
changes is which stored account it authenticates as.

An external store's login is a `credentials` row, and a managed one's
keys are its own — generated here, on its own row. That split is the
difference between a secret this instance was *given* and one it
*minted*: only the first is worth naming and reusing, and only the first
makes this module a `credential.Dependant`.

Keys typed while linking become an account in the same transaction, so a
credential stays a **convenience and not a prerequisite** — nobody's
first act on a fresh instance is naming an account.

**Nothing is checked against the endpoint at link time.** The credential
may be scoped to one bucket and the endpoint may be behind a network
this daemon reaches later; a refusal there would be Cubeship deciding a
store is broken on evidence it does not have. Whether the login works is
answered the first time somebody opens it, where the provider's own
words can be shown — and `ErrDenied`, `ErrUnreachable`,
`ErrBucketNotFound` keep "the store said no" apart from "you may not",
which are 403 and 403 and mean entirely different things to whoever
reads them.

A store may be **pinned to one bucket**, for a login that reaches one
and cannot list them. It is pinned in both directions: that bucket is
reported without asking the endpoint, and reaching any other is refused
here rather than by a provider's access-denied that nobody can act on.

**It is offered on every provider**, and the form is what changes.
`Provider.ScopesByBucket` says where a per-bucket login is the ordinary
kind — R2's tokens and a Space's access keys, both of which their
consoles hand out that way — and there the field is the expected answer.
Everywhere else it is the unusual one, and the hint says what it costs:
naming a bucket gives up listing, creating and deleting for the *whole
store*.

**That was a refusal for one release, and the refusal was wrong.** The
field was accepted only where `ScopesByBucket` was true, on the grounds
that a field offered everywhere is one somebody fills in because it is
there. What it actually did was decide, on somebody else's behalf, what
their credential can reach: an IAM policy narrowed to one bucket is
ordinary, a compatible endpoint can be anything at all, and a store
linked with such a key and no way to say so has nothing to show. It also
bought nothing where the field *was* offered — the cost was never
mentioned there.

Saying what it costs and taking the answer is the same trade this
product makes about revoking a last API key: **knowing rather than
refusing**, with the way back one click away.

Two things it is still not. A **managed** store lists its own buckets,
so pinning one is a limit invented out of nothing (`ErrManagedFixed`).
And a bucket name S3 itself would reject is refused where it is typed,
by `CheckBucketName`, like every other value here that becomes part of a
request.

**And it moves afterwards, from the store's settings.** `PATCH
/objectstores/{name}` takes `bucket`, and empty is what **unpins** —
a value rather than a gap, so leaving the field out is the only way of
saying "as it is" and saving a description cannot unpin a store by
omission. That is what makes the field safe to offer: pinning is a
decision somebody can take back without re-linking anything.

Pinning is also refused when **an attached app names a different bucket**
(`ErrAttachedElsewhere`, 409, with the app references). The app itself
is unaffected — its keys reach the endpoint directly and `S3_BUCKET`
comes from the attachment — so what would break is the screen: a store
claiming to be one bucket while an attachment names another, and no way
to browse the bucket an app is actively using. Refused with the names,
the same shape as deleting a credential something stands on.

### The role, and where the line is

Managing a store is an admin's. So is **everything inside one**, and
that is the decision worth stating: Cubeship lets a member read a great
deal about what the instance is wired to and never lets one read *data*
— there is no way to see a row of anybody's database from here either —
and a bucket is data. A member deploying an app still uses the store,
because the app is given the keys and the app is what reads the bucket.

The MCP tools stop one step short of that again: they describe the
storage and never its contents. An agent can see that
`backups/2026-09-01.sql.gz` exists and how big it is, and cannot open
it, create a store, link one, or publish one.

Attaching is the exception, and it is the one that pays for the rule:
wiring an app to a bucket hands over no secret at any point, because
the keys reach the app through its own environment. That is the same
line `datastore` draws, and it is what lets an agent finish the job
rather than stop one step short of it.

### The managed half

A container beside the databases, named `cubeship-s3-<slug>`, with its
objects in a host bind mount under `<data dir>/objectstores/<id>` — the
same shape as a datastore, including that the slug is the container's
name and therefore permanent, and that a version is permanent because a
data directory belongs to the server that wrote it.

The image is pinned, and the pin is not a chore: MinIO's community image
stopped moving, so the tag is where the free server ends rather than a
snapshot of something stale next month. One directory, which is
single-node single-drive mode — erasure coding wants several drives and
there is one disk on this machine.

**Exposing it publishes a host port**, from 16000-16999. Deliberately
not the datastores' 15000-15999: both modules check only their own
table, and an overlap would surface as a container that will not bind,
minutes later, for no reason visible on either screen.
`TestTheAutomaticPortRangeCannotCollideWithADatabase` pins that.

There is **no Traefik router and no domain**, though MinIO speaks HTTP
and one would work. A hostname has to be unique across every app on the
instance, and that is a decision `app_domains` owns; borrowing it here
would mean two modules writing router labels for names neither can see.
So an exposed store is a plain port with no TLS, the endpoint says so,
and a firewall rule is what makes it safe.

### How an app reaches one

By being **attached** to a bucket. An attachment gives the app six
variables, from its next deploy onwards — a container keeps the
environment it was created with, the same rule that makes adding a
domain take effect on redeploy:

```
S3_ENDPOINT  S3_REGION  S3_BUCKET
S3_ACCESS_KEY_ID  S3_SECRET_ACCESS_KEY  S3_PATH_STYLE
```

**The bucket is on the attachment**, not on the store. A store holds
many and an app wants one — `S3_BUCKET` has to have a value — which is
also why one app may be attached to the same store twice: a bucket for
uploads and one for backups is an ordinary shape, and the prefix keeps
their variables apart.

**There is no URL**, because no S3 client agrees on one, and **no
`AWS_*` names**. Those would make an app using the AWS SDK work with no
configuration at all, and would also mean two stores on one app
fighting over six names the SDK reads and a prefix does not reach. The
mapping is one line in an app's own environment when it wants it.

`S3_PATH_STYLE` is there because half the S3 clients in the world have
to be told and the other half guess wrong.

The unique index is `(app_id, prefix)` and nothing else. The datastores'
carries the engine's stem as well, because there a Redis and a Postgres
write different middle words and do not collide at one prefix; every
attachment here writes the same six names, so the prefix is the whole of
the namespace — and a column that would always hold `S3` says nothing.
A datastore's attachment cannot collide with one of these either, which
is the point of both stems.

The seam is `app.ObjectStoreVars`, declared in `app` and satisfied here
— a second interface beside `app.DatastoreVars` rather than one list of
contributors, because each is labelled with its own `envvar.Source` and
"where did this come from" has to answer "a database" or "a bucket"
rather than "something attached".

**The bucket is not checked against the store when attaching.** Whether
it exists is a live call this instance may not be allowed to make — a
credential scoped to one bucket may not stat another — and refusing on
evidence it does not have is how a working attachment gets blocked. The
screen offers the buckets it can list; the API takes the name.

### What is not here

**Public buckets.** Nothing here sets a bucket policy, so a store is
reached with a key or not at all.
