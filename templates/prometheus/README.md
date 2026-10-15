# Prometheus on Cubeship

[Prometheus](https://prometheus.io) is the CNCF's monitoring system: it scrapes
metrics from the services that expose them, stores them as time series, and
answers queries in PromQL — the data source most Grafana dashboards are built
on.

This template installs one Prometheus server on a Cubeship instance, reachable
only by the apps on that instance, with its time series in a volume. It pairs
with the [Grafana template](https://github.com/cubeshipd/cubeship/tree/master/templates/grafana).

## What it creates

- **prometheus** — Prometheus `v3.14.0`, built on the instance from the
  `Dockerfile` in this repository. It listens on port `9090` inside the
  instance, has no domain, and keeps its database in a volume at `/prometheus`.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `prom/prometheus`, reads its targets from
`/etc/prometheus/prometheus.yml`, and the one it ships scrapes nothing but
Prometheus itself. Cubeship cannot mount a file into a container, so the
`Dockerfile` here is that image with [`prometheus.yml`](prometheus.yml) in the
example's place, run with the image's own flags. To scrape your apps, change
that file — see [Changing what it scrapes](#changing-what-it-scrapes).

## What you are asked

Nothing. There is no domain to choose and no account to create.

## Why it has no domain

Prometheus has no sign-in: anyone who reaches it can read every metric and run
any query. Its one protection is basic auth in a web configuration file, with
the passwords written as bcrypt hashes, and a template input cannot produce a
hash. Cubeship's proxy adds no authentication of its own, so a domain would put
all of it on the internet. Apps on the instance reach it at its internal
address instead, and that is all Grafana needs.

For the same reason the remote-write receiver stays off: accepting samples
pushed from elsewhere needs a domain, and one anybody could write to.

## After installing

Add it to Grafana as a data source:

1. In Grafana, open *Connections → Data sources → Add data source* and choose
   *Prometheus*.
2. Set *Prometheus server URL* to
   `http://cubeship-prometheus-production-prometheus:9090`. The internal name
   follows the project, environment and app names you install with; it is on
   the app's page in the dashboard.
3. Leave authentication off and choose *Save & test*.

Grafana's *Explore* then runs PromQL against it. `up` lists every target and
whether its last scrape worked.

To open Prometheus's own UI, forward a port over SSH to the machine the app
runs on:

```bash
# on the VPS: the container's address
docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' \
  $(docker ps -qf name=cubeship-prometheus-production-prometheus)

# on your laptop, then open http://localhost:9090
ssh -L 9090:<container address>:9090 <you>@<your VPS>
```

## Changing what it scrapes

Out of the box Prometheus scrapes itself. `prometheus.yml` shows, commented
out, the shape for an app on the instance: its internal address,
`cubeship-<project>-<environment>-<app>`, on the port its metrics are served
on, with `metrics_path` when that is not `/metrics`.

The file is built into the image, so a change is a new build:

1. Fork this repository and edit `prometheus.yml` in your fork.
2. Connect the GitHub account the fork is on, under the instance's GitHub
   settings, if it is not connected already.
3. On the app's *Settings → Source*, choose the fork and the branch or tag to
   build, save, and deploy. An admin can do this; a member cannot, because the
   app builds.

With a branch chosen, every push to it deploys again. A tag deploys only when
you ask. Each deploy restarts Prometheus; nothing scraped before is lost.

## Choices this template makes

- **Retention of 15 days or 2 GB, whichever comes first.** Set in
  `prometheus.yml` under `storage.tsdb.retention` — the command-line flags for
  it are deprecated. Prometheus's own disk use can briefly go past the size
  while it compacts, so keep more free than that on the machine. A few dozen
  targets at a 15-second interval fit well inside it; raise both when you add
  many.
- **Scraping every 15 seconds**, and evaluating rules as often. There are no
  rules and no Alertmanager: alert from Grafana, or add `rule_files` and
  `alerting` to your fork.
- **No reload endpoint.** `--web.enable-lifecycle` would let any app on the
  instance shut Prometheus down, and the configuration changes only with a
  deploy anyway.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds, during which nothing is scraped: those samples are gaps,
not errors. Everything Prometheus stored is in `/prometheus`; back it up from
the app's settings if the history matters to you.

## Updating

Change the tag in the `Dockerfile`, release this repository — or your fork —
and point the app's ref at the new release.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Memory grows with the number
of series scraped, not with retention: raise `limits` in `template.yaml` before
adding targets with thousands of series each.
