# Inngest on Cubeship

[Inngest](https://www.inngest.com) is a durable execution engine: your app
defines functions as steps, and Inngest triggers them from events, retries each
step on failure, and handles schedules, delays, concurrency and fan-out, all
without a queue of your own.

This template installs the self-hosted Inngest server on a Cubeship instance,
reachable only by the apps on that instance, with the managed Postgres and
Redis it keeps its state in.

## What it creates

- **inngest** — Inngest `v1.44.0`, built on the instance from the `Dockerfile`
  in this repository. The event API, the API your apps sync with and the
  dashboard all answer on port `8288` inside the instance. It has no domain.
- **inngest-db** — a managed Postgres 18 database: apps, functions, events and
  run history.
- **inngest-redis** — a managed Redis 7.4: the queue and the state of runs in
  progress. Cubeship runs Redis with the append-only file on, so a restart
  does not lose queued work.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `inngest/inngest`, runs `inngest` with no command, which
prints the help and exits. Cubeship runs an image's own command, so the
`Dockerfile` here is that image running `inngest start`, the self-hosted
server. Everything else is the `INNGEST_*` variables in `template.yaml`: the
two keys, and Postgres and Redis in place of the SQLite file and in-memory
Redis Inngest falls back to, which a deploy would empty.

## What you are asked

| Input | What to give |
| --- | --- |
| The key apps send events with | Nothing — the instance generates it and shows it once. |
| The key Inngest and your apps sign requests with | The output of `openssl rand -hex 32`. Inngest refuses to start unless it is hexadecimal with an even number of characters, and a generated secret is letters and digits. |

Keep both: every app that uses Inngest needs them.

## Why it has no domain

Inngest's dashboard has no sign-in. The GraphQL API it runs on answers anyone
who reaches the port, and it lists every event with its payload and every run
with its step output, and can replay, rerun and cancel them. The signing key
protects only what your apps call — syncing, `/v1` and `/v2` — and the event
key only sending events. Cubeship's proxy adds no authentication of its own,
so a domain would put all of it on the internet.

Without one, events come only from apps on the instance. To take events from
outside — a webhook, another server — receive them in an app of yours that has
a domain, and send them on to Inngest from there.

## Connecting an app

Set these on the app, in its settings or at its project or environment level:

| Variable | Value |
| --- | --- |
| `INNGEST_BASE_URL` | `http://cubeship-inngest-production-inngest:8288` |
| `INNGEST_EVENT_KEY` | the event key |
| `INNGEST_SIGNING_KEY` | the signing key |
| `INNGEST_DEV` | `0` |

The internal name follows the project, environment and app names you install
with; it is on the app's page in the dashboard.

Then **sync the app**, so Inngest learns its functions. Inngest calls the app
back at the address it is given, so that address must be the app's internal
one — `http://cubeship-<project>-<environment>-<app>:<port>/api/inngest`, or
wherever the app serves the Inngest handler:

- from the dashboard: *Apps → Sync new app*, with that URL; or
- by sending a `PUT` to the app's own handler, which makes the SDK register
  with `INNGEST_BASE_URL`.

Sync again after deploying changed functions. Inngest does not poll apps for
changes unless `INNGEST_POLL_INTERVAL` is set, in seconds.

A Go app on SDK v0.16 or newer also needs `INNGEST_ENABLE_UNAUTHED_SYNC=true`
for a sync started from the dashboard.

## Opening the dashboard

Forward a port over SSH to the machine the app runs on:

```bash
# on the VPS: the container's address
docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' \
  $(docker ps -qf name=cubeship-inngest-production-inngest)

# on your laptop, then open http://localhost:8288
ssh -L 8288:<container address>:8288 <you>@<your VPS>
```

Cubeship has no console into an app, so the `inngest` CLI's own diagnostics
are over SSH too: `docker exec` into that container and run
`inngest alpha doctor`.

## Connect

Inngest's `connect` mode, where a worker keeps a WebSocket open to the server
on port `8289` instead of being called over HTTP, is not set up here. Serve
your functions over HTTP as above.

## Health

`/health` answers `200` without a key once the server is up.

## What it does not do

Inngest does not delete old events, runs or traces from Postgres. On a busy
instance the tables grow until the dashboard slows down; watch the database's
size.

## Updating

Change the tag in the `Dockerfile`, release this repository, and point `ref` at
the new release. Back the database up first: Inngest migrates its schema on
start.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Inngest runs 100 queue
workers by default; raise `limits` in `template.yaml` for heavy workloads, and
set `INNGEST_QUEUE_WORKERS` to change the count.
