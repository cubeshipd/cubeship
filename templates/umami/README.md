# Umami on Cubeship

[Umami](https://umami.is) is a simple, privacy-focused web analytics tool — an
open-source alternative to Google Analytics that stores no cookies and keeps
your visitors' data on your own server.

This template installs it on a Cubeship instance with the managed Postgres it
needs.

## What it creates

- **web** — the Umami dashboard and tracking endpoint, from
  `ghcr.io/umami-software/umami:3.3.1`, answering on the domain you choose.
- **umami-db** — a managed Postgres 18 database, attached to the app, so
  `DATABASE_URL` is set for you.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the dashboard answers | A domain you control, pointed at your instance. |
| The app's session secret | Nothing — the instance generates it and shows it once. |

## After installing

1. Open the domain and sign in with `admin` / `umami`.
2. **Change that password straight away** under *Settings → Profile*.
3. Add a website and paste the tracking snippet into your site.

## Two-factor authentication

Umami only offers it with `TWO_FACTOR_ENCRYPTION_KEY` set to 64 hex
characters, which this template cannot generate. To turn it on, add the
variable to the `web` app yourself and redeploy:

```bash
openssl rand -hex 32
```

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Raise `limits` in
`template.yaml` if your traffic needs more.
