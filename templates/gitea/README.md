# Gitea on Cubeship

[Gitea](https://about.gitea.com) is a lightweight self-hosted Git service:
repositories, pull requests, issues, wikis, packages and a container registry,
in one Go binary.

This template installs it on a Cubeship instance, with the managed Postgres it
keeps users, issues and settings in, and volumes for the repositories and its
configuration.

## What it creates

- **gitea** — Gitea, from `gitea/gitea:1.27.3-rootless`, answering on the
  domain you choose, with two volumes:
  - `/var/lib/gitea` — every repository, LFS objects, attachments and avatars.
  - `/etc/gitea` — `app.ini`, including the internal token and signing
    secrets Gitea generates on its first start.
- **gitea-db** — a managed Postgres 16 database, holding users,
  organizations, issues, pull requests and settings.

It needs Cubeship 0.7.0 or newer.

The web installer is skipped: the database, the URL and the secret key are
set by the app's variables, and `INSTALL_LOCK` is on.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Gitea answers | A domain you control, pointed at your instance. It also becomes `ROOT_URL`. |
| The key Gitea encrypts stored secrets with | Nothing — the instance generates it and shows it once. **Keep a copy**: a new key cannot read two-factor secrets and other values encrypted under the old one. |

## After installing

1. **Open the domain straight away and register.** The first account
   registered becomes the site administrator. Until you do, that is anyone who
   finds the domain.
2. **Close registration**, unless you want anyone to sign up. On the `gitea`
   app, set `GITEA__service__DISABLE_REGISTRATION` to `true` and redeploy.
   From the CLI:

   ```bash
   cubeship app env set gitea/production/gitea GITEA__service__DISABLE_REGISTRATION=true
   cubeship app deploy gitea/production/gitea
   ```

   Add people afterwards under *Site Administration → User Accounts*.

To create the administrator instead of registering one, run Gitea's own
command over SSH on the machine the app runs on — Cubeship has no console into
an app:

```bash
docker exec -it $(docker ps -qf name=cubeship-gitea-production-gitea) \
  gitea admin user create --admin --username <name> --email <address> --random-password
```

It prints the password; change it after signing in. The same
`gitea admin user change-password --username <name> --password <new>` resets a
forgotten one.

## Git over HTTPS only

Clone, pull and push over `https://<your domain>/<owner>/<repo>.git`. **Git
over SSH does not work**: it needs a TCP port — `22` or `2222` — reachable from
outside, and Cubeship cannot expose a port other than a domain's HTTP. SSH is
turned off, so Gitea shows only HTTPS clone URLs and does not offer SSH keys.

Push with your password, or with an access token made under
*Settings → Applications* — required once two-factor sign-in is on. Git LFS
works over the same URL.

## Settings in variables

Any `app.ini` setting can be set on the app as
`GITEA__<section>__<KEY>` — `GITEA__mailer__ENABLED`, `GITEA__service__REQUIRE_SIGNIN_VIEW`
— and applied by redeploying. Gitea writes them into `app.ini` on every start,
so a variable wins over anything edited in the file. See upstream's
[configuration cheat sheet](https://docs.gitea.com/administration/config-cheat-sheet).

## Mail

No mail is set up, and Gitea runs without it — but then nobody gets
notifications, and a forgotten password is only reset over SSH. To add it, set
these on the app and redeploy:

| Variable | Value |
| --- | --- |
| `GITEA__mailer__ENABLED` | `true` |
| `GITEA__mailer__PROTOCOL` | `smtp+starttls` for port `587`, `smtps` for `465`. |
| `GITEA__mailer__SMTP_ADDR` | Your provider's host. |
| `GITEA__mailer__SMTP_PORT` | `587` or `465`. |
| `GITEA__mailer__USER` | From your provider. |
| `GITEA__mailer__PASSWD` | From your provider. |
| `GITEA__mailer__FROM` | An address your provider lets you send as. |

## Actions

Gitea Actions is on, but no runner is included, so workflows stay queued. The
runner, `act_runner`, starts a container for every job and needs the Docker
socket, which Cubeship does not give an app. Run it on another machine with
Docker, registered against `https://<your domain>` with a token from
*Site Administration → Actions → Runners* — see upstream's
[act_runner guide](https://docs.gitea.com/usage/actions/act-runner). To hide
Actions instead, set `GITEA__actions__ENABLED` to `false`.

## The volumes

The app runs as one copy on the machine its volumes are on, and a deploy stops
it for a few seconds, during which nobody can push or pull. Back up both
volumes and the database together: the repositories are in one, and who owns
them and every issue about them is in the other.

## Resources

The app is limited to 1 CPU and 1 GiB of memory, comfortable for a small
team. Large repositories, many mirrors or code search indexing need more:
raise `limits` in `template.yaml`.
