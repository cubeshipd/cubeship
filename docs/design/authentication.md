# Authentication, and claiming an instance

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

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
