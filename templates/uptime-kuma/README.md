# Uptime Kuma on Cubeship

[Uptime Kuma](https://uptime.kuma.pet) is a self-hosted uptime monitor: it
checks websites, ports, DNS records, containers and databases on a schedule,
notifies you when one goes down, and publishes status pages.

This template installs it on a Cubeship instance, with its data kept in a
volume.

## What it creates

- **uptime-kuma** — Uptime Kuma, from `louislam/uptime-kuma:2.5.4`, answering
  on the domain you choose, with a volume at `/app/data`: the SQLite database
  holding monitors, their history, users and settings, and the images uploaded
  to status pages.

It needs Cubeship 0.7.0 or newer.

No managed database is created. Uptime Kuma can use MariaDB instead, but it
would still need the volume for uploads, so SQLite is one app and one volume
instead of two things to keep in step.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Uptime Kuma answers | A domain you control, pointed at your instance. |

## After installing

1. **Open the domain straight away.** Uptime Kuma has no default account: the
   first person to open it creates the admin. Until you do, that is anyone who
   finds the domain.
2. Under *Settings → General*, set *Primary Base URL* to `https://<your domain>`,
   so links in notifications point back at it.
3. Under *Settings → Reverse Proxy*, set *Trusted Proxies* to *Yes*, so logins
   and logs show the visitor's address rather than the proxy's.
4. Add a monitor. An app on the same instance is reachable at its internal
   address, `cubeship-<project>-<environment>-<app>`, on its own port.

A forgotten password is reset over SSH on the machine the app runs on, since
Cubeship has no console into an app:

```bash
docker exec -it $(docker ps -qf name=cubeship-uptime-kuma-production-uptime-kuma) npm run reset-password
```

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds, during which no checks run. Back the volume up from the
app's settings — every monitor and its history is in it.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Hundreds of monitors, or
*Real Browser* monitors, need more: raise `limits` in `template.yaml`.
