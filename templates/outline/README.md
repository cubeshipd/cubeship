# Outline on Cubeship

[Outline](https://www.getoutline.com) is an open-source team wiki and
knowledge base: documents in collections, real-time collaborative editing,
search, and sharing.

This template installs it on a Cubeship instance with the managed Postgres and
Redis it needs, and a volume for uploaded files.

## What it creates

- **outline** — Outline, from `outlinewiki/outline:1.10.1`, answering on the
  domain you choose, with a volume at `/var/lib/outline/data`: images and
  attachments uploaded to documents, avatars and imports.
- **outline-db** — a managed Postgres 16 database, attached to the app.
  Documents, users and settings are in it.
- **outline-redis** — a managed Redis 7.4, attached to the app. Outline's job
  queues and the live collaboration between editors run through it.

It needs Cubeship 0.7.0 or newer.

## Signing in needs an identity provider

Outline has no usernames or passwords. Everyone signs in through a provider,
and a fresh instance without one shows a sign-in page with no way in. This
template configures a generic **OpenID Connect** provider, which is required.

[Cubeship's Keycloak template](https://github.com/cubeshipd/cubeship/tree/master/templates/keycloak)
works. In its admin console:

1. Create a realm for your team, such as `outline`, and the users in it. Each
   needs an email address: Outline identifies people by it.
2. Under *Clients*, create an *OpenID Connect* client with the client ID
   `outline`. Turn on *Client authentication*, keep *Standard flow*.
3. Set *Valid redirect URIs* to `https://<your Outline domain>/auth/oidc.callback`
   and *Web origins* to `https://<your Outline domain>`.
4. Copy the secret from the client's *Credentials* tab.

The issuer URL is then `https://<your Keycloak domain>/realms/outline`. Outline
reads the rest from its discovery document when it starts, so Keycloak must be
reachable at that address first.

Google, Microsoft Entra, Slack and Discord sign-in are also built into
Outline. To use one instead, set its variables on the `outline` app — see
Outline's [`.env.sample`](https://github.com/outline/outline/blob/main/.env.sample)
for their names and a link to each one's guide — and redeploy. The OIDC
variables are documented in [Outline's OIDC guide](https://docs.getoutline.com/s/hosting/doc/oidc-8CPBm6uC0I).

## What you are asked

| Input | What to give |
| --- | --- |
| Where Outline answers | A domain you control, pointed at your instance. It becomes `URL`, the address in every link Outline writes. |
| The key Outline encrypts stored data with | The output of `openssl rand -hex 32`, run on any machine. |
| The secret for Outline's internal task endpoint | Nothing — the instance generates it. |
| Your OpenID Connect issuer URL | The provider's issuer, like `https://sso.example.com/realms/outline`. |
| The client ID | The client you created, like `outline`. |
| The client secret | From the client's credentials. |
| The name on the sign-in button | What people click, like `Keycloak` or your company's name. |
| Your SMTP server | Optional. The host your mail provider gives you. |
| Its TLS port | `465` unless your provider says otherwise. |
| The SMTP username | Optional. Leave it empty for a server with no authentication. |
| The SMTP password | Optional. |
| The address Outline sends from | Optional. An address your provider lets you send as. |

**The encryption key is typed, not generated.** Outline requires exactly 64
hexadecimal characters and decodes them as 32 bytes; the instance generates
letters and digits, which Outline refuses to start with. Keep a copy. Outline
encrypts integration tokens and other columns with it, and a changed key makes
them unreadable and signs everybody out.

Mail is optional. Without it Outline starts and works, but sends nothing:
no invitations, no notifications of changes and comments, no email sign-in
links. Leaving the SMTP server empty turns email off. Outline connects to port
`465` with TLS from the start; for a provider that only offers STARTTLS on
`587`, set the port to `587`, add `SMTP_SECURE` set to `false` on the `outline`
app and redeploy.

## After installing

1. **Sign in straight away.** The first person to sign in creates the
   workspace and becomes its admin.
2. Anyone else who can sign in through the provider joins the workspace as a
   member. Decide who that is in the provider — the users in the Keycloak
   realm — or restrict it by email domain under *Settings → Security* in
   Outline.

The sign-in and mail settings are variables on the `outline` app. Change them
and redeploy.

Cubeship has no console into an app. Anything that needs a shell inside
Outline is done over SSH on the machine it runs on, with `docker exec` into
its container.

## Files

Uploads are stored on the app's volume, `FILE_STORAGE=local`, and pass
through Outline itself.

A managed object store is not used on purpose. With `FILE_STORAGE=s3`,
Outline hands the browser a signed URL and the browser uploads straight to the
bucket, so the bucket must be reachable from every reader's browser at a public
address. A managed MinIO store on Cubeship is reachable by apps on the
instance, not from the internet, and uploads would fail.

The volume starts owned the way the image leaves `/var/lib/outline/data`:
writable by the `nodejs` user, uid 1001, that Outline runs as.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Back up both the volume, for uploaded files, and the
database, for everything else: one without the other is a wiki with broken
images or no documents.

## Health

The health check is `/_health`, which answers `200` once Outline reaches both
Postgres and Redis, and `500` when either is down. It answers before Outline's
redirect from HTTP to HTTPS, so the plain HTTP probe is not redirected.

That redirect stays on. TLS ends at Cubeship's proxy, which tells Outline the
request arrived over HTTPS in `X-Forwarded-Proto`, and Outline trusts that
header by default, so browsers are not sent round in a loop.

## Resources

The app is limited to 1 CPU and 2 GiB of memory. One process runs the web
server, the background worker and live collaboration together. Raise `limits`
in `template.yaml` for a large team or big imports.

## Updating

Change `tag` on the `outline` app and redeploy. Outline migrates its database
on start, and there is no going back: back the database up first, and read the
[release notes](https://github.com/outline/outline/releases).
