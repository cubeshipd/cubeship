# Plausible on Cubeship

[Plausible](https://plausible.io) is a simple, privacy-friendly web analytics
tool: no cookies, no personal data, and a single-page dashboard. This template
installs Plausible Community Edition, the self-hosted edition, on a Cubeship
instance.

## What it creates

- **plausible** — Plausible CE `v3.2.1`, built on the instance from
  [`plausible/Dockerfile`](plausible/Dockerfile). The dashboard and the
  tracking script answer on the domain you choose.
- **clickhouse** — ClickHouse `24.12.6.70`, built from
  [`clickhouse/Dockerfile`](clickhouse/Dockerfile), where Plausible keeps every
  pageview and event. It has no domain, listens on port `8123` inside the
  instance, and keeps its data in a volume at `/var/lib/clickhouse`.
- **plausible-db** — a managed Postgres 18 database, attached to Plausible:
  users, sites, goals and settings.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: both apps are
built on the instance, and only admins build. ClickHouse also needs a CPU with
SSE 4.2 (x86) or NEON (ARM), which any recent VPS has.

## Why they are built

Both published images are used as they are, with what Plausible's own
[compose file](https://github.com/plausible/community-edition/blob/v3.2.1/compose.yml)
adds to them and Cubeship cannot:

- **Plausible** — compose replaces the command with
  `db createdb && db migrate && run`. The image's own command is only `run`,
  which starts on an empty database and fails. The Dockerfile makes those
  three steps the command, so every start, and so every deploy, creates what
  is missing and migrates before the server starts.
- **ClickHouse** — compose mounts four configuration files from
  [`clickhouse/`](clickhouse): logs to the console, the system logs Plausible
  never reads switched off, IPv4 only, and smaller caches and one thread per
  query for a small machine. Cubeship cannot mount a file, so the Dockerfile
  copies them in.

Compose also raises the open-files limit, which Cubeship cannot set on a
container. ClickHouse raises its own limit to the host's maximum when it
starts, and Plausible's command raises its limit to `65535` where the host
allows.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Plausible answers | A domain you control, pointed at your instance. It becomes `BASE_URL`, and the address of the tracking script. |
| The key sessions are signed with | Nothing — the instance generates it. |
| The key two-factor secrets are encrypted with | The output of `openssl rand -base64 32`. **Keep a copy.** |
| The password Plausible uses for ClickHouse | Nothing — the instance generates it. |
| Your SMTP server | The host your mail provider gives you, like `smtp.mailgun.org`. |
| Its port | `587` unless your provider says otherwise. |
| The SMTP username | From your mail provider. |
| The SMTP password | From your mail provider. |
| The address Plausible sends from | An address your provider lets you send as. |

**The two-factor key is typed, not generated.** Plausible reads it as Base64
of exactly 32 bytes, and the instance only generates letters and digits, which
never decode to that. Changing it later makes every enabled two-factor setup
unreadable.

**Mail is not optional.** Invites are the only way to add people once the first
account exists, and password resets and email reports need it too. Plausible
reads an empty variable as a value rather than as unset, so a blank SMTP answer
would stop it starting. On port `465` Plausible connects with TLS from the
first byte; on any other it connects in plain and relies on the server's
STARTTLS.

## After installing

1. **Open `https://<your domain>` straight away.** Plausible has no default
   account: while it has no users, it sends everyone to registration, and the
   first person to register owns the instance.
2. Add your website, then paste the snippet Plausible shows into the `<head>`
   of every page. It loads the script from your domain:

   ```html
   <!-- Privacy-friendly analytics by Plausible -->
   <script async src="https://<your domain>/js/pa-XXXXXXXX.js"></script>
   <script>
     window.plausible=window.plausible||function(){(plausible.q=plausible.q||[]).push(arguments)},plausible.init=plausible.init||function(i){plausible.o=i||{}};
     plausible.init()
   </script>
   ```

   The `pa-…` name is per site; copy it from the site's settings, not from
   here.

After the first account, registration is by invite only — Plausible's default.
To open or close it completely, set `DISABLE_REGISTRATION` on the `plausible`
app to `false` or `true` and redeploy.

## Reaching the data

Cubeship has no console into an app. Anything that needs a shell is done over
SSH on the machine the app runs on, with `docker exec`. For example, to query
the events with ClickHouse's client and the generated password:

```bash
docker exec -it $(docker ps -qf name=cubeship-plausible-production-clickhouse) \
  clickhouse-client --user plausible --password '<password>' \
  --database plausible_events_db
```

The ClickHouse password is `CLICKHOUSE_PASSWORD` on the `clickhouse` app. The
internal names follow the project, environment and app names you install
with; they are on each app's page in the dashboard.

## Countries and cities

Plausible ships a db-ip country database in its image, so visitor countries
work without any setup. For regions and cities, create a free
[MaxMind](https://www.maxmind.com) account and set `MAXMIND_LICENSE_KEY` on the
`plausible` app, then redeploy; see Plausible's
[MaxMind guide](https://github.com/plausible/community-edition/wiki/maxmind-integration).
Plausible downloads the database into the container, which has no volume, so it
downloads it again after each deploy.

Google Search Console and Google Analytics imports need `GOOGLE_CLIENT_ID` and
`GOOGLE_CLIENT_SECRET`; see the
[Google guide](https://github.com/plausible/community-edition/wiki/google-integration).

## The volume

ClickHouse runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds, during which Plausible cannot record events. Back up both the volume, for the statistics, and
the database, for the accounts and sites they belong to.

## Resources

ClickHouse is limited to 1 CPU and 2 GiB of memory. It reads the container's
limit and caps its own use at 90% of it, so a query that needs more fails with
a memory error rather than the container being killed. Plausible is limited to 1 CPU and 1 GiB. Plausible recommends at least 2 GiB
for the two together; raise ClickHouse's `limits` in `template.yaml` first for
busy sites or long date ranges.

## Updating

Change the tag in `plausible/Dockerfile`, release this repository, and point
both apps' `ref` at the new release. Plausible migrates on start, and there is
no going back: back the database and the volume up first, and read the
[release notes](https://github.com/plausible/analytics/releases) and
[upgrade guide](https://github.com/plausible/community-edition/wiki/upgrade).
Move ClickHouse only to the version Plausible's compose file names for that
release.
