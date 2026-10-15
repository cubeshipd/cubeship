# Hermes Agent on Cubeship

[Hermes Agent](https://github.com/NousResearch/hermes-agent) is Nous Research's
open-source AI agent: it runs tools, a terminal and a browser on the machine it
lives on, keeps memories and skills between sessions, and talks to you from its
web dashboard or from Telegram, Discord, Slack and other chat platforms.

This template installs it on a Cubeship instance, with its dashboard on a
domain and everything it keeps in a volume.

## What it creates

- **hermes** — Hermes Agent `v2026.9.21`, built on the instance from the
  `Dockerfile` in this repository. The dashboard answers on the domain you
  choose; config, API keys, sessions, memories, skills and logs are kept in a
  volume at `/opt/data`.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `nousresearch/hermes-agent`, starts an interactive chat
when given no command, and exits when there is no terminal. Cubeship runs an
image's own command, so the `Dockerfile` here is that image with one line
changed: `gateway run`. The gateway runs supervised, keeps the container up,
and brings the dashboard with it.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the dashboard answers | A domain you control, pointed at your instance. |
| The username you sign in with | Anything; `admin` unless you change it. |
| The password you sign in with | Nothing — the instance generates it and shows it once. **Keep a copy.** |
| The key sign-in sessions are signed with | Nothing — the instance generates it. |

No model provider key is asked for. Enter one in the dashboard instead: it is
written to the volume, and a key the template set would be an empty variable
on the app whenever you left it blank.

## After installing

1. Open the domain and sign in with the username and the generated password.
2. Under *API Keys*, add a key for a model provider — `OPENROUTER_API_KEY`,
   `ANTHROPIC_API_KEY`, or another listed there — and choose a model under
   *Config*.
3. Talk to it under *Chat*. If a chat platform you configure does not
   answer, redeploy the app so the gateway reads the new settings.
4. To reach it from a chat platform, add that platform's bot token under
   *Channels* **and the user IDs allowed to use it**. Anyone who can message
   the bot can run commands on the instance.

## Its dashboard is on the internet

The dashboard holds your API keys and can run anything Hermes can, and the
password is all that stands in front of it. Hermes documents its
username/password sign-in as meant for a trusted network, not the public
internet. Use a long password, keep the domain to yourself, and consider its
OAuth or OIDC sign-in (`HERMES_DASHBOARD_OAUTH_CLIENT_ID`, or
`HERMES_DASHBOARD_OIDC_ISSUER` with `HERMES_DASHBOARD_OIDC_CLIENT_ID`) for
anything shared.

The gateway's OpenAI-compatible API server is off. Turning it on
(`API_SERVER_ENABLED`, `API_SERVER_HOST=0.0.0.0` and `API_SERVER_KEY`) makes it
reachable only inside the instance, on port `8642`: the domain goes to the
dashboard.

## What Hermes can reach

Hermes runs its terminal commands inside its own container, as an unprivileged
user. It has no Docker socket, so its Docker terminal backend does not work
here, and it can reach whatever else the container can: the internet, and
other apps on the instance at their internal addresses.

## Updating Hermes

The Hermes version is the `FROM` line of the `Dockerfile`, at the `ref` the
template builds. The dashboard's update button cannot update a container in
place; a new release of this template can.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds: chat platforms reconnect, and a task running at that
moment is cut off. Back the volume up from the app's settings: your API keys
and the agent's memory are in it.

## Resources

The app is limited to 2 CPUs and 4 GiB of memory; the browser tool is the
hungry part. Raise `limits` in `template.yaml` if you need more.

---

<!-- cubeship-crosslink -->

## About Cubeship

This is a template for [**Cubeship**](https://github.com/cubeshipd/cubeship) —
a PaaS you run on your own server: `docker push`, and it is live, with HTTPS,
a database beside it, and a second machine when one stops being enough.

Browse every template at [cubeship.dev/templates](https://cubeship.dev/templates).
