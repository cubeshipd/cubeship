# Grafana on Cubeship

[Grafana](https://grafana.com/oss/grafana/) turns metrics, logs and traces from
the data sources you already run into dashboards and alerts.

This template installs it on a Cubeship instance with a managed Postgres for
its dashboards, users and alert rules.

## What it creates

- **grafana** — the Grafana server, from `grafana/grafana:13.2.1`, answering on
  the domain you choose.
- **grafana-db** — a managed Postgres 18 database, attached to the app.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Grafana answers | A domain you control, pointed at your instance. |
| The password for the admin account | Nothing — the instance generates it and shows it once. |
| The key Grafana encrypts data source credentials with | Nothing — generated and shown once. **Keep a copy**: a new key cannot read the passwords saved under the old one. |

## After installing

1. Open the domain and sign in as `admin` with the generated password.
2. Add your first data source under *Connections*.

`GF_SERVER_ROOT_URL` assumes the instance serves HTTPS. On an instance with
TLS off, change it to start with `http://`.

## Plugins

The app has no disk of its own, so a plugin installed from the UI is gone on
the next deploy. List the ones you need in `GF_PLUGINS_PREINSTALL` instead —
for example `grafana-clock-panel` — and Grafana installs them on every start.

## Resources

The app is limited to 1 CPU and 512 MiB of memory. Raise `limits` in
`template.yaml` for many users or heavy dashboards.
