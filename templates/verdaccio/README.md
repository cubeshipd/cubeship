# Verdaccio on Cubeship

[Verdaccio](https://verdaccio.org) is a private npm registry: publish your own
packages to it, and install public ones through it, cached from npmjs.

This template installs one Verdaccio on a Cubeship instance, with its web UI and
registry API on a domain, registration closed, and every package — its own and
the cached ones — kept in a volume.

## What it creates

- **verdaccio** — Verdaccio `6.10.3`, built on the instance from the
  `Dockerfile` in this repository. The registry and its web UI answer on the
  domain you choose; packages and accounts are kept in a volume at
  `/verdaccio/storage`.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `verdaccio/verdaccio`, reads its configuration from
`/verdaccio/conf/config.yaml`, and the one it ships lets anyone register an
account and anyone read every package. Cubeship cannot mount a file into a
container, so the `Dockerfile` here is that image with
[`config.yaml`](config.yaml) in its place, and one step before the image's own
command: [`htpasswd-add`](htpasswd-add) creates the admin account from the
inputs, the first time the container starts.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the registry answers | A domain you control, pointed at your instance. It also becomes `VERDACCIO_PUBLIC_URL`, the address tarball links point at. |
| The admin's user name | `admin` unless you want another. |
| The admin's password | Nothing — the instance generates it and shows it once. **Keep a copy.** |

The user name and password are read once, when the volume has no accounts yet.
Changing the variables afterwards changes nothing; see
[Accounts](#accounts) to change a password.

## After installing

Point npm at the registry and sign in with the admin account:

```bash
npm set registry https://npm.example.com/
npm login --registry https://npm.example.com/
```

`npm login` asks for the user name and the generated password, and saves a
token for that registry in `~/.npmrc`. Then publish and install as usual:

```bash
npm publish
npm install react   # fetched from npmjs through Verdaccio, and cached
```

The web UI on the domain shows nothing until you sign in there too.

`npm adduser --registry https://npm.example.com/` never creates an account
here: registration is closed, and Verdaccio answers a new name with *409
registration is disabled*. Use `npm login` with an account that exists.

## Accounts

Accounts live in `/verdaccio/storage/htpasswd`, in the volume. Verdaccio has
**no way to create one over its API or UI while registration is closed**, and
Cubeship has no console into an app, so another account is added over SSH, on
the machine the app runs on:

```bash
# the password is read from standard input, so it stays out of shell history
read -rs PASS && printf '%s\n' "$PASS" | docker exec -i \
  $(docker ps -qf name=cubeship-verdaccio-production-verdaccio) htpasswd-add alice
```

The same command with an existing name changes that account's password — the
way to change the admin's too. Verdaccio reads the file on every sign-in, so
nothing restarts. To remove an account, delete its line:

```bash
docker exec $(docker ps -qf name=cubeship-verdaccio-production-verdaccio) \
  sed -i '/^alice:/d' /verdaccio/storage/htpasswd
```

Removing an account, or changing its password, also ends the tokens issued to
it; see [Tokens](#tokens).

The container name follows the project, environment and app names you install
with; it is on the app's page in the dashboard.

### Why registration is closed

Verdaccio's configuration file does not read environment variables, so the
number of accounts allowed cannot be an input. Its other supported way to let
exactly one person register, `max_users: 1`, would leave the registry open to
whoever reaches the domain first after install — and that person could then
publish packages every CI job installs. So `max_users` is `-1`: nobody
registers, and the first account is written before the server starts.

## CI tokens

A CI job needs a token, not a password. Sign in once as the account the job
should use (an account of its own is better than the admin):

```bash
npm login --registry https://npm.example.com/
grep npm.example.com ~/.npmrc
# //npm.example.com/:_authToken="…"
```

Store that token as a secret in CI, and give the job an `.npmrc` that reads it:

```ini
registry=https://npm.example.com/
//npm.example.com/:_authToken=${NPM_TOKEN}
```

For only a scope from Verdaccio, keeping everything else on npmjs:

```ini
@your-company:registry=https://npm.example.com/
//npm.example.com/:_authToken=${NPM_TOKEN}
```

### Tokens

Verdaccio's default tokens carry the account's name and password, encrypted
with a secret it creates in the volume on first start. They have no expiry,
but every request with one is checked against the htpasswd file again: a token
stops working when its account is removed or its password changes. That is
also the way to revoke a leaked CI token — change the password of the account
it belongs to, and sign in again where it is still needed.

For tokens that expire on their own, add a `security.api.jwt` block to
`config.yaml` in a fork — see
[Security](https://verdaccio.org/docs/configuration#security).

## Who can read what

Every package, published here or cached from npmjs, needs a signed-in account
to read (`access: $authenticated`), and any account can publish and unpublish.
Anonymous reads stay off because they would serve private packages to anyone
who guesses a name.

To open reads to everyone, change `access` to `$all` in `config.yaml` in a
fork, for `'**'` and `'@*/*'`, or add a pattern above them for the packages that
should be public. Packages match the first pattern that fits.

Every name not published here is looked up on npmjs. For your own scope, add a
pattern without `proxy` above the two, as `config.yaml` shows commented out: a
missing package in it then fails instead of being fetched from npmjs, where
anybody could publish that name.

## Changing the configuration

`config.yaml` is built into the image, so a change is a new build:

1. Fork this repository and edit `config.yaml` in your fork.
2. Connect the GitHub account the fork is on, under the instance's GitHub
   settings, if it is not connected already.
3. On the app's *Settings → Source*, choose the fork and the branch or tag to
   build, save, and deploy. An admin can do this; a member cannot, because the
   app builds.

## Choices this template makes

- **Packages at `/verdaccio/storage/data`**, the image's own default, next to
  `htpasswd` in the same volume.
- **The upload limit stays at Verdaccio's 10 MB** per publish. Raise
  `max_body_size` in a fork for larger packages.
- **`npm audit` is passed through to npmjs**, as in the image's configuration.

## Health

Traefik checks `/-/ping`, which answers `200` without signing in.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds, during which installs through it fail. Everything is in
`/verdaccio/storage`: published packages, the npmjs cache, the accounts and the
token secret. Back it up from the app's settings. The cache grows with every
public package installed through it; deleting a package's directory under
`data/` drops it from the cache, and the next install fetches it again.

## Updating

Change the tag in the `Dockerfile`, release this repository — or your fork —
and point the app's ref at the new release. Stay on `6.x`: Verdaccio 7 is not
released as stable yet.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Raise `limits` in
`template.yaml` if you need more.
