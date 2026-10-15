# Mattermost on Cubeship

[Mattermost](https://mattermost.com) is an open-source team chat: channels,
direct messages, threads, file sharing and integrations, with desktop and
mobile apps, running on your own server.

This template installs Mattermost Team Edition on a Cubeship instance, with
the managed Postgres it keeps messages and its configuration in, and a volume
for uploaded files.

## What it creates

- **mattermost** — Mattermost Team Edition, from
  `mattermost/mattermost-team-edition:11.7.10`, answering on the domain you
  choose, with a volume at `/mattermost/data`: every uploaded file and image,
  and the bundles of the plugins you install.
- **mattermost-db** — a managed Postgres 18 database holding users, teams,
  channels, messages and the server's configuration.

It needs Cubeship 0.7.0 or newer.

11.7 is Mattermost's Extended Support Release, supported until May 2027, and
the version upstream's own Docker setup ships. Feature releases come monthly
and are supported for three months each; an ESR is supported for a year, and
upgrading from one ESR to the next is what upstream tests.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Mattermost answers | A domain you control, pointed at your instance. It also becomes the *Site URL*. |

## After installing

1. **Open the domain straight away.** Mattermost has no default account: the
   first person to sign up becomes the system admin. Until you do, that is
   anyone who finds the domain.
2. Create your team, then invite people from its menu. After the first
   account, nobody can sign up without an invite link unless you open the
   server under *System Console → Authentication → Signup*.
3. In the desktop and mobile apps, give `https://<your domain>` as the server
   URL.

The *Site URL* and the database connection are set by the app's variables,
so they are greyed out in the System Console. To move to another domain,
change `MM_SERVICESETTINGS_SITEURL` on the `mattermost` app and redeploy.

## Configuration

Everything else changed in the System Console is kept in the database, not in
`config.json`: `MM_CONFIG` points Mattermost at Postgres for its configuration,
so it survives deploys without a volume of its own.

## Mail

No mail is set up, and Mattermost runs without it — but then nobody can reset
a forgotten password, verify an address or get email notifications. Set it
under *System Console → Environment → SMTP*, and turn on
*Site Configuration → Notifications → Enable Email Notifications*.

## Calls

Voice and video calls **will not work**. The Calls plugin ships with
Mattermost, but it carries media on port `8443`, UDP or TCP, straight to the
server — not through the domain — and Cubeship cannot expose a port other than
a domain's HTTP. Making it work needs the
media service reachable some other way: Mattermost's standalone `rtcd` on a
machine with port `8443` open, or a TURN server such as coturn that clients
can reach — see upstream's
[Calls deployment guide](https://docs.mattermost.com/administration-guide/configure/calls-deployment-guide.html).
Team Edition's calls are limited to one-to-one and 40 minutes even then.

## Mobile push notifications

A new server sends mobile push notifications through Mattermost's free *Test
Push Notification Service*, `https://push-test.mattermost.com`. It works out
of the box but comes with no uptime guarantee, and upstream does not recommend
it for production. The production *Hosted Push Notification Service* needs a
paid license; the alternative is to run your own push proxy with your own
builds of the mobile apps. Either is set under
*System Console → Environment → Push Notification Server*.

## Diagnostics

Mattermost sends usage statistics and error reports to Mattermost, Inc. by
default. Turn it off under
*System Console → Environment → Logging → Enable Diagnostics and Error Reporting*,
or set `MM_LOGSETTINGS_ENABLEDIAGNOSTICS` to `false` on the app and redeploy.

## Resetting a password

A forgotten password is reset over SSH on the machine the app runs on, since
Cubeship has no console into an app. The image has no shell, but it carries
Mattermost's `mmctl`, which talks to the running server directly:

```bash
docker exec -it $(docker ps -qf name=cubeship-mattermost-production-mattermost) \
  mmctl --local user change-password <username> --password '<new password>'
```

The same `mmctl --local` makes a user a system admin
(`user create … --system-admin`), lists users, and more.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds, during which nobody can connect. Back up the volume and
the database together: messages are in one and the files they link to are in
the other.

Server logs go to the app's log output. The copies Mattermost also writes under
`/mattermost/logs` are not kept across deploys, and neither is a Bleve search
index, which is off by default.

## Resources

The app is limited to 1 CPU and 2 GiB of memory, what Mattermost recommends for
up to 1,000 users. Raise `limits` in `template.yaml` if you need more.
