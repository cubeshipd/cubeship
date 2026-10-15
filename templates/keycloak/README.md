# Keycloak on Cubeship

[Keycloak](https://www.keycloak.org) is an open-source identity and access
manager: single sign-on over OpenID Connect and SAML, user federation, social
login and fine-grained authorization for your apps.

This template installs one Keycloak server on a Cubeship instance, with the
managed Postgres it keeps realms, users and sessions in.

## What it creates

- **keycloak** — Keycloak `26.7.3`, built on the instance from the `Dockerfile`
  in this repository. The login pages, account console, admin console and
  OIDC/SAML endpoints answer on the domain you choose.
- **keycloak-db** — a managed Postgres 18 database, attached to the app.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `quay.io/keycloak/keycloak`, has `kc.sh` as its
entrypoint, and `kc.sh` with no command starts no server. Cubeship
runs an image's own command, so the `Dockerfile` here is Keycloak's own
[optimized container](https://www.keycloak.org/server/containers): a build
stage runs `kc.sh build` with the build-time options — Postgres as the
database, health checks on — and the image runs `start --optimized`, which
skips that work on every start.

Everything else is runtime configuration, set in `template.yaml`: the database
connection, the hostname, plain HTTP on port `8080` with the `X-Forwarded-*`
headers trusted — TLS ends at Cubeship's proxy — and the temporary admin.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Keycloak answers | A domain you control, pointed at your instance. It becomes `KC_HOSTNAME`, the address in every token and link Keycloak issues. |
| The temporary admin's username | `admin` unless you want another. |
| The temporary admin's password | Nothing — the instance generates it and shows it once. |

## After installing

1. Open `https://<your domain>/admin` and sign in to the master realm with the
   temporary admin.
2. **Create a permanent admin**: under *Users*, add a user, set a password on
   the *Credentials* tab, and give it the `admin` role under *Role mapping*.
3. Sign out, sign in as the new admin, and delete the temporary one. Keycloak
   marks it temporary in the console until you do.
4. Create a realm for your apps rather than using `master`, which is for
   administering Keycloak itself.

The temporary admin is only created the first time Keycloak starts, while the
master realm does not exist. Changing the two variables later does nothing.

If you lose admin access, Cubeship has no console into an app, so recovery is
over SSH on the machine the app runs on: stop the app, then run
`kc.sh bootstrap-admin user` in a one-off container from the app's image, on
the instance's network, with the app's `KC_DB_*` variables — see Keycloak's
[admin recovery guide](https://www.keycloak.org/server/bootstrap-admin-recovery).

## Reaching it from other apps

Point clients at `https://<your domain>/realms/<realm>`; the discovery document
is at `/realms/<realm>/.well-known/openid-configuration`. Apps on the same
instance should use the domain too: tokens carry the domain as their issuer, so
a client that reaches Keycloak at its internal name,
`cubeship-keycloak-production-keycloak:8080`, rejects them.

## Health

Keycloak's `/health/ready` and `/health/live` answer on the management port,
`9000`, which Cubeship does not probe and the domain does not expose. The
app's health check is `/realms/master` on port `8080` instead: the master
realm's public key and endpoints, answered without signing in once Keycloak
has started and reached the database.

## Resources

The app is limited to 2 CPUs and 2 GiB of memory, which Keycloak recommends
for a small production server; the image sizes the Java heap to 70% of the
limit. Raise `limits` in `template.yaml` for many users or realms.

## Updating

Change the tag in both stages of the `Dockerfile`, release this repository, and
point `ref` at the new release. Read Keycloak's
[upgrading guide](https://www.keycloak.org/docs/latest/upgrading/) and back the
database up first: Keycloak migrates its schema on start, and there is no going
back.
