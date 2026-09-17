# Authentication, and claiming an instance

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../../AGENTS.md).

## Authentication

Two credentials reach the same middleware. An **API key** is what a CLI
or an MCP client carries in `Authorization: Bearer`; a **session cookie**
is what a browser carries. The header is tried first, so a request
sending both meant the key.

**A cookie is not enough on its own for a state-changing request.**
`SameSite=Lax` is the first line and does not reach far enough: an app
deployed here answers at `app.example.com` while the dashboard is at
`example.com`, so the two are *same-site* and the cookie is sent —
anyone who can push an app could otherwise host a page that acts as
whoever visits it. So the session branch of the middleware requires
`httpx.SameOrigin`: `Sec-Fetch-Site` where the browser sends it, `Origin`
compared by host otherwise, neither being a refusal. `httpx.DecodeJSON`
is the other half — a body must be declared as JSON, because
form-encoded, multipart and `text/plain` are exactly the three a browser
will send cross-site without a preflight. Safe methods and API keys are
untouched: a key is not a credential a browser attaches by itself.

Sessions are rows, not signed cookies, because they have to be revocable:
logging out ends one, and changing a password ends every other one the
account holds. Only the token's hash is stored, like an API key's.

**Two hashes, and the difference is not cosmetic.** API keys and session
tokens are 32 bytes of randomness, so `authkey.Hash` (SHA-256) is right —
guessing them is hopeless whatever the hash costs. Passwords are chosen
by people, so they go through Argon2id with the parameters recorded
alongside each hash. Never hash a password with `authkey.Hash`.

The session cookie's `Secure` flag follows the request rather than being
hard-coded: a fresh install is reached at `http://<ip>:3000`, and a
Secure cookie there is simply never sent back — the sign-in would appear
to work and nothing would stay signed in. `SameSite=Lax` is what stands
in for CSRF tokens.

**An account's credentials can be revoked by an admin, and so can the
account.** `DELETE /users/{username}/credentials` ends every session and
revokes every API key one account holds and leaves the account — the
answer to a laptop that walked off, where what was on the machine has to
stop working everywhere at once. The password is not touched: it is a
secret in somebody's head rather than a credential lying on the machine
that was lost. `DELETE /users/{username}` is the person leaving: the
account goes and its keys and sessions go with it, in one transaction,
so nothing that authenticates outlives the account it belonged to. Two
refusals: the account you are signed in as, and the last admin — setup
closed when the first account appeared, and nothing in the API can make
an admin without one.

**Revoking a key is never refused, including the last one.** It used to
be, to stop somebody locking themselves out — and that is the wrong
trade the moment you name the case revocation exists for. A key that has
leaked has to be able to go *now*; under the old rule the answer to
"this key is in somebody else's hands" was to mint a second one first,
which leaves the leaked one live for as long as that takes and is a
strange thing to be made to do in a hurry.

What replaces it is knowing rather than refusing, which is the same
shape as every other irreversible act here: `/users/me` carries
`has_password`, so the account screen can say what revoking this one
costs — the CLI until you make another, or the way in — and ask. A
confirmation in front of it, not a refusal to work around.

An account can exist with no password — one made before this instance
issued them, or made straight through the repository, which is what
every test does. Nothing in the API produces one any more. That case is
still why every sign-in failure — unknown username, wrong password, no
password at all — is the same answer, and why an unknown username still
pays for a hash verification.

### Shutting somebody out, and letting them back

**Blocking is the reversible half of deleting.** Deleting takes the
account, its keys and its sessions in one transaction and cannot be
undone — the username is free for anybody to claim again, and nothing
remembers what the account was. Somebody suspended for a week, or an
account being looked into, needed a door that closes and opens, and the
only answer before this was to delete and make it again.

So **nothing is revoked**. A blocked account keeps its password, its
keys and the sessions it is signed in on, and every one of them is
refused at the door instead: `Login`, `Authenticate` and
`AuthenticateSession` all answer `ErrBlocked`. Revoking would make
unblocking a half-undo — the account would come back with nothing to
come back with, and an admin who blocked the wrong person for a minute
would have cost them every key on every machine they own.

`blocked_at` is a timestamp rather than a flag, because when it happened
is the first thing anybody asks and a boolean costs the same to store.
Null is not blocked, which is every account that existed before it.

**`ErrBlocked` is deliberately not `ErrInvalidCredentials`**, and it is
answered *after* the password is verified. The credential is fine and
the account is not — telling somebody their password is wrong when it is
right sends them to reset the one thing that was never the problem —
and checking it after the hash keeps a wrong password on a blocked
account from saying the account exists, which every other failure here
is shaped to avoid.

### Resetting somebody else's password

`POST /users/{username}/password` issues one and returns it **once**,
for an admin to hand over. This box sends no mail, so there is no reset
link and an account that has forgotten its password has nothing else to
try; what people did instead was delete the account and make it again,
losing its keys and its history to recover a secret.

**The API keys are not touched**, which is the whole difference between
it and `DELETE /users/{username}/credentials`. A forgotten password is
not a lost laptop: the keys on somebody's machine are still theirs, and
taking them as well turns a two-minute fix into a morning of logging
back into everything. Whoever wants both asks for both.

Every session ends, because the password changed — the same rule an
account changing its own follows.

### One rule, three acts

Deleting an account, demoting it and blocking it are three ways of
taking the last admin off an instance, so they ask one question:
`Repository.refuseIfLastAdmin`, counted **inside** the transaction.

Sequentially it is unreachable, and that is worth knowing rather than
discovering: only an admin may do any of these, so the only way to
target the last admin is to be them — at which point the refusal that
fires is the one about your own account. What the count is for is the
case no single request can show, two admins taking each other's role in
the same moment, where a check made outside the transaction lets both
through.

## Access roles, and what everyone did

**Roles are the authorization model; `admin` and `member` are its two
ends.** An access role (`access_roles`, `internal/user/role.go`) is a name
and a list of grants: a resource, a level (`none`/`view`/`manage`),
whether it reads secrets, and which items — project slugs, database or
store names — with **null meaning every one and an empty list none**, so
a grant whose items were all deleted reaches nothing.

`user.Authenticate` and `AuthenticateSession` put a `Policy` on the
caller: nil for an admin, the member's role or `DefaultMemberPolicy`, and
either intersected with the key's role. `Effective()` answers the member
default for a member whose Policy was never set — **nil is everything only
for an admin**, because a member built by hand without it passed every
check once. Every module asks `user.Allow(caller, resource, level, item)`;
an item outside a grant is `ErrHidden`, which modules answer as their own
not found. Secrets are `AllowSecrets`: app, project and environment
variables, database and store credentials, a bucket's contents, backup
downloads.

The resources split where the old line between member and admin ran, so
`DefaultMemberPolicy` is exactly what a member had: **apps** (deploys,
variables, volumes) apart from **projects** (their structure and
variables) and **domains**. Seeing a project follows from seeing its apps.
Users and roles are not a resource — whoever can grant access can grant
themselves all of it — and building source stays the owner's admin role
(`app.requireSource`), whatever a key's role says.
Project and environment variable writes also require that owner's admin
role, in addition to `projects:manage` on the project. Every inherited
variable can feed a build, including Railpack commands, so this applies
to replacements, merges and removals regardless of the apps' current
sources. Checking only existing build apps would allow a member to plant
commands before a later source change. A restricted admin key still needs
the project grant; a member with that grant cannot write inherited build
input.

A key's role only narrows. A restricted key cannot mint a key whose
effective policy is wider than its own, revoke keys or change the
password. A role held by anybody cannot be deleted (`ErrRoleInUse`, and
`ON DELETE RESTRICT` under a race).

**A shell is a grant of its own.** `Shell` on apps, beside `Secrets`,
needing manage as well and never implied by either — every role written
before shells has manage and secrets, and implying it would have given a
shell to every deployer on the day of the upgrade. A root shell on a
machine is an unrestricted admin's and no grant reaches it; from the
dashboard it also asks for the password again (`ConfirmPassword`,
throttled like a sign-in). See [shell.md](shell.md).

**Over MCP a tool the caller cannot use is removed from the list** rather
than refused (`server/access.go`): an agent cannot be talked into calling
what it was never shown. `toolRules` names each tool's resource and level,
and a test fails for one that is not classified. The services decide every
call regardless.

**The audit log** (`internal/audit`) records at the same two doors: every
non-read HTTP request behind `auth`, every refused one including reads,
and every tool call that changes something or was not available. Recorded
inside authentication, so refusals are in it. Never a body; a tool's
arguments keep only the ones that name something. What no request asked
for — a push webhook's deploy, a schedule — is not in it. Kept 90 days.

## Claiming an instance

`internal/setup` is the first-run flow, and it exists because the daemon
starts with no account at all. `POST /setup` creates the account, signs
the caller in, and closes setup permanently: `Needed` is "are there zero
users", so the *first* account is the only one setup ever makes, and it
is an admin.

That check and the insert are one transaction behind
`pg_advisory_xact_lock`, because two people opening the page at once must
not both succeed and the username's unique index would not stop them —
they may well pick different names. The loser gets `ErrAlreadySetUp`
(409).

**Nothing else is created.** There is no organization to invent any more,
and a project is something you make when you have something to put in it
— a slug is permanent, so a name picked on someone's behalf is one they
are stuck with. The projects screen opens empty, saying so and offering
the button.

**Claiming it takes the setup token**, which the daemon writes to
`setup-token` in the data directory on its first start and the installer
prints. Without it the installer publishes a port and whoever reaches it
first is the admin of the machine — a race the operator can lose between
running the install command and opening their browser. The data
directory is root-only, so the token makes claiming the instance take
access to the host, which is what it always meant to require.
`setup.EnsureToken` keeps the one it wrote across restarts — a new token
every start would invalidate the one the installer printed — and removes
it the moment setup succeeds, because a credential that can no longer do
anything should not be left in a directory that gets backed up.

The account gets a **password and no API key** — its way in is the
session setup starts. A key nobody is ever shown would be a live
credential lying around for nothing; keys are self-service.

Password login admits at most two simultaneous attempts per daemon,
with a token bucket of ten attempts refilling at one per second. It
rejects overload immediately with 429 and Retry-After rather than queuing
password hashes. Login bodies are limited to 16 KiB, have a ten-second
read deadline, and accept at most 255 username bytes and 1024 password
bytes. Password creation and verification share the password limit;
older passwords exceeding it require an administrator reset. Other JSON
request bodies are limited to 1 MiB, including public setup requests.
Header and idle timeouts protect connections without imposing a global
body timeout on streaming operations.


## Profile images

`PUT /users/me/avatar` and `DELETE /users/me/avatar` change only the authenticated
caller's picture, under the existing session/API-key and same-origin mutation
checks. Uploads are raw PNG, JPEG or WebP bytes, bounded to 512 KiB and sniffed
on the server; SVG and caller-provided MIME claims are not accepted.

Migration 00058 adds `user_avatars`, one bounded BYTEA per user. Small identity
images live with the account database, so backup and deletion have one lifecycle;
the foreign key cascades when an account is deleted. Upload and removal update
the image and the version in `users.avatar` in one SQL statement, locking the
user row first. Ordinary profile edits leave that version alone unless the
legacy avatar preference was explicitly sent.

Identity responses expose `avatar_url` when an upload exists. The URL names the
current username and includes a content hash; rename responses carry the updated
URL. `GET /users/{username}/avatar` is authenticated and available to signed-in
users, including `me` as an alias. It serves private, revalidated images with
ETag and nosniff; unset images return 404. New accounts have no picture, and the
dashboard uses initials. Legacy preset names are still accepted for older API
clients but are no longer offered or displayed in the dashboard.
