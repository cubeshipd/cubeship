# Vaultwarden on Cubeship

[Vaultwarden](https://github.com/dani-garcia/vaultwarden) is a lightweight
server for the Bitwarden password manager, written in Rust: the official
Bitwarden browser extensions, desktop and mobile apps sync against it, and it
serves the web vault itself.

This template installs it on a Cubeship instance, with its data kept in a
volume.

## What it creates

- **vaultwarden** — Vaultwarden, from `vaultwarden/server:1.37.3`, answering on
  the domain you choose, with a volume at `/data`: the SQLite database holding
  every vault, the attachments and Send files, the site icon cache, and the RSA
  keys that sign sessions.

It needs Cubeship 0.7.0 or newer.

No managed database is created. Vaultwarden can use Postgres instead, but
attachments, Sends and its keys would still need the volume, so SQLite — which
upstream recommends for most installs — is one app and one volume instead of
two things to back up in step.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the vault answers | A domain you control, pointed at your instance. It also becomes `DOMAIN`. |

The vault needs HTTPS: browsers only give the web vault the cryptography it
encrypts with over a secure connection, and `DOMAIN` is set to
`https://<your domain>`. On an instance with TLS off, it will not work.

## After installing

1. **Open the domain straight away and create your account.** Sign-ups are
   open so that you can: until you close them, anyone who finds the domain can
   create one too. Your master password encrypts the vault and cannot be
   recovered — nobody, including the server, can reset it.
2. **Close sign-ups.** On the `vaultwarden` app, set `SIGNUPS_ALLOWED` to
   `false` and redeploy. From the CLI:

   ```bash
   cubeship app env set vaultwarden/production/vaultwarden SIGNUPS_ALLOWED=false
   cubeship app deploy vaultwarden/production/vaultwarden
   ```

3. In the Bitwarden apps, choose *self-hosted* when signing in and give
   `https://<your domain>` as the server URL.

To add people later, create an organization and invite them to it — invitations
still work with sign-ups closed, and an invited address can register at
`https://<your domain>/#/signup` even without mail set up.

## The admin page

`/admin` is off: no `ADMIN_TOKEN` is set. It lets whoever holds the token list
and delete users, invite people and change settings, so turn it on only when
you need it.

Cubeship has no console into an app, so make the token on your own machine.
Choose a long password, then turn it into an Argon2 hash with Vaultwarden's own
command — any machine with Docker will do:

```bash
docker run --rm -it vaultwarden/server:1.37.3 /vaultwarden hash
```

It asks for the password twice and prints `ADMIN_TOKEN='$argon2id$…'`. Set
`ADMIN_TOKEN` on the `vaultwarden` app to the part between the quotes — without
the quotes — and redeploy. From the CLI, keep the single quotes so the shell
leaves the `$` alone:

```bash
cubeship app env set vaultwarden/production/vaultwarden 'ADMIN_TOKEN=$argon2id$v=19$…'
cubeship app deploy vaultwarden/production/vaultwarden
```

Sign in at `https://<your domain>/admin` with the
password, not the hash. A plain password in `ADMIN_TOKEN` works too, but then
anyone who can read the app's variables can sign in, and Vaultwarden warns
about it on every start.

**Saving anything on the admin page writes `/data/config.json`, which overrides
the app's variables from then on** — including `SIGNUPS_ALLOWED` and `DOMAIN`.
Change those settings in one place: the variables, or the admin page. To turn
the page off again, remove `ADMIN_TOKEN` and redeploy.

## Mail

No mail is set up, and Vaultwarden runs without it. Mail is what sends
invitations, verifies addresses, carries email two-factor codes and the
new-device notice, and sends a password hint when asked for one.

To add it, set these on the `vaultwarden` app and redeploy:

| Variable | Value |
| --- | --- |
| `SMTP_HOST` | Your provider's host, like `smtp.mailgun.org`. |
| `SMTP_FROM` | An address your provider lets you send as. |
| `SMTP_PORT` | `587`, or `465`. |
| `SMTP_SECURITY` | `starttls` for `587`, `force_tls` for `465`. |
| `SMTP_USERNAME` | From your provider. |
| `SMTP_PASSWORD` | From your provider. |

Set them all or none: Vaultwarden refuses to start with `SMTP_HOST` present and
`SMTP_FROM` missing, and an empty `SMTP_HOST` still counts as present.

## Notifications

Browser, desktop and extension clients get live updates over a WebSocket on the
same domain; nothing extra is needed. The mobile apps use push notifications
relayed through Bitwarden's servers, which need an installation id and key from
[bitwarden.com/host](https://bitwarden.com/host) — see upstream's
[push notification guide](https://github.com/dani-garcia/vaultwarden/wiki/Enabling-Mobile-Client-push-notification).
Without it the mobile apps still sync, just not instantly.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Back the volume up from the app's settings, and keep the
backup somewhere as safe as the vault: everything is in it. Losing the RSA keys
in it signs every client out.

## Updating

Bitwarden's clients update themselves, and Vaultwarden follows their changes.
Keep the tag current — change it on the app and redeploy — or an old server can
stop working with new clients.

## Resources

The app is limited to 1 CPU and 512 MiB of memory, plenty for a family or a
small team. Raise `limits` in `template.yaml` if you need more.
