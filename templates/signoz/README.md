# SigNoz on Cubeship

[SigNoz](https://signoz.io) is an open-source observability platform built on
OpenTelemetry: traces, metrics and logs in one UI, stored in ClickHouse, with
dashboards and alerts.

This template installs a single-node SigNoz on a Cubeship instance: the UI on
one domain, an OpenTelemetry collector that apps on the instance send to
directly and senders elsewhere reach on a second domain with a token, and the
ClickHouse it all lives in.

> **SigNoz needs a machine with at least 8 GB of memory and 4 CPU cores.**
> Its apps are limited to 6 GiB between them, plus its Postgres database, and
> ClickHouse uses what it is given. On a smaller VPS, lower `limits` in
> `template.yaml` and expect queries over more than a few days to fail.

## What it creates

- **clickhouse** — ClickHouse `25.12.5`, built on the instance from
  [`clickhouse/Dockerfile`](clickhouse/Dockerfile), with ClickHouse Keeper
  running inside it. No domain; its data, and Keeper's, are in a volume at
  `/var/lib/clickhouse`.
- **collector** — SigNoz's OpenTelemetry collector `v0.144.9`, built from
  [`collector/Dockerfile`](collector/Dockerfile). It migrates ClickHouse's
  schema when it starts, then receives OTLP: gRPC on `4317` and HTTP on
  `4318` inside the instance, gRPC on a published TCP port, and HTTP with a
  bearer token on the ingest domain.
- **signoz** — SigNoz `v0.141.1`, the published `signoz/signoz` image: the
  API, the UI and the alert manager, on the domain you choose.
- **signoz-db** — a managed Postgres 16 database, where SigNoz keeps users,
  dashboards, alerts and settings.

These are the versions SigNoz's own Docker install,
[Foundry](https://github.com/SigNoz/foundry), runs together.

It needs Cubeship 0.7.2 or newer, and **an admin to install it**: two of its
apps are built on the instance, and only admins build.

## Why two apps are built

SigNoz's Docker install runs seven containers, three of which Cubeship cannot
run as they are:

- Two run once and exit: one fetches `histogramQuantile`, a function
  ClickHouse runs for percentile queries, and one migrates ClickHouse's schema.
  Cubeship has no one-off containers; an app that exits is a failed app.
- ClickHouse, Keeper and the collector read configuration files, and Cubeship
  cannot mount a file into a container.

So `clickhouse/Dockerfile` is ClickHouse with the function and
[`config.xml`](clickhouse/config.xml) built in, and `collector/Dockerfile` is
the collector with [`config.yaml`](collector/config.yaml) and
[`start.sh`](collector/start.sh), which runs the migrations before the
collector.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the SigNoz UI answers | A domain you control, pointed at your instance. |
| Where telemetry from outside the instance is sent | A second domain, for OTLP over HTTP from anywhere else. |
| The port OTLP gRPC answers on | `4317` by default. Choose a port from `1024` to `65535`, and open it in your provider's firewall too. |

Three secrets are generated and shown once — keep the first:

- **The ingest token**, which senders outside the instance put in an
  `Authorization: Bearer` header.
- **ClickHouse's password**, which the collector and SigNoz connect with.
- **The session secret** SigNoz signs sign-ins with.

## After installing

1. Open the UI domain. The first account you create is the admin of the
   organization; do it before anyone else reaches the page.
2. Send telemetry. Nothing shows until something does.

## Sending telemetry

**From an app on the same instance**, point its OpenTelemetry SDK or collector
at the collector's internal address. No token:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://cubeship-signoz-production-collector:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_SERVICE_NAME=my-app
```

Or gRPC, on `4317`. The internal name follows the project, environment and app
names you install with; it is on the app's page in the dashboard.

**From anywhere else, over HTTP**, send OTLP to the ingest domain with the
token:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=https://otel.example.com
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer%20<ingest token>"
```

A request without the token is refused with `401`. **For OTLP gRPC**, connect
to the instance's address on the published port you chose. The TCP port does
not add authentication or TLS, so protect it with your provider's firewall or
use a secured network path.

## How the schema is migrated

SigNoz's schema is created and upgraded by the collector's `migrate` command.
On every start, `start.sh` waits up to ten minutes for ClickHouse and its
Keeper, runs `migrate bootstrap` and `migrate sync up`, starts
`migrate async up` in the background — as upstream runs it beside a collector
that is already receiving — and then starts the collector. Each step does
nothing when there is nothing to do, so a new collector version migrates on its
first start after the deploy.

If the synchronous part fails, the collector exits and Cubeship restarts it,
and the migration runs again. The collector's log says why.

The collector then **receives nothing until SigNoz connects to it** over
OpAMP, on port `4320` of the signoz app: until then it runs with every
pipeline switched off, as upstream's does. On a fresh install that is the
minute between the collector starting and SigNoz answering. A running
collector keeps its pipelines if SigNoz goes down; one that restarts while
SigNoz is down waits for it again.

## ClickHouse, on one node

SigNoz creates every table `ON CLUSTER cluster`, replicated, and both need a
coordinator — upstream runs ClickHouse Keeper in a container of its own.
Here Keeper runs inside the ClickHouse server, which ClickHouse supports, and
the cluster named `cluster` is that one server. It is the same schema a
larger SigNoz runs, with one replica.

Keeper's port, `9181`, has no authentication, and like ClickHouse's own ports
it is reachable by other apps on the instance, not from outside it.
ClickHouse itself asks for the generated password.

## Retention

SigNoz keeps logs and traces for 7 days and metrics for 30. Change it in the
UI under *Settings → General*. Disk use grows with what you send; watch the
volume's machine.

## The volume

The clickhouse app runs as one copy on the machine its volume is on, and a
deploy stops it for a few seconds: the collector's exports fail and retry, and
the UI's queries fail, until it is back. Everything SigNoz received is in
`/var/lib/clickhouse`. What SigNoz itself keeps — accounts, dashboards,
alerts — is in `signoz-db`, backed up like any managed database.

## Updating

Change the image tags — `clickhouse/Dockerfile`, `collector/Dockerfile`,
`tag` in `template.yaml` — to a set SigNoz's
[Foundry](https://github.com/SigNoz/foundry) releases together, release this
repository or your fork, and point both built apps' ref at the new release.
Deploy the collector before `signoz`, so the schema is migrated first.

## Resources

ClickHouse is limited to 2 CPUs and 4 GiB of memory, the collector and SigNoz
to 1 CPU and 1 GiB each. ClickHouse sizes its caches from its limit; raise it
first when queries over long ranges fail for memory.
