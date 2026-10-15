# n8n on Cubeship

[n8n](https://n8n.io) is a workflow automation tool: connect APIs, databases
and services with a visual editor, and run the workflows on your own server.

This template installs it on a Cubeship instance with the managed Postgres it
keeps workflows, executions and credentials in.

## What it creates

- **n8n** — the editor and webhook endpoint, from `n8nio/n8n:2.38.7`,
  answering on the domain you choose.
- **n8n-db** — a managed Postgres 18 database, attached to the app.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the editor answers | A domain you control, pointed at your instance. |
| The key n8n encrypts stored credentials with | Nothing — the instance generates it and shows it once. **Keep a copy**: a new key cannot read credentials saved under the old one. |

## After installing

1. Open the domain and create the owner account.
2. Webhooks are served at `https://<your domain>/webhook/…`.

The URLs assume the instance serves HTTPS. On an instance with TLS off, change
`N8N_PROTOCOL` to `http` and `WEBHOOK_URL` to start with `http://`.

## What is not kept

The app has no disk of its own, so anything n8n writes outside Postgres is
gone on the next deploy — community nodes installed from the editor, and
binary data if you switch it to filesystem mode. Workflows, executions and
credentials are in the database and stay.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Raise `limits` in
`template.yaml` for heavy workflows.
