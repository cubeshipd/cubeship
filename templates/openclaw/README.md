# OpenClaw on Cubeship

[OpenClaw](https://github.com/openclaw/openclaw) — formerly Clawdbot and
Moltbot — is an open-source personal AI assistant. It runs an agent with tools
and a shell on the machine it lives on, keeps memory and sessions between
conversations, and talks to you from its web Control UI or from WhatsApp,
Telegram, Discord, Slack and other chat platforms.

This template installs its gateway on a Cubeship instance, with the Control UI
on a domain and everything it keeps in a volume.

## What it creates

- **openclaw** — OpenClaw `2026.9.4`, built on the instance from the
  `Dockerfile` in this repository. The Control UI answers on the domain you
  choose; config, credentials, sessions, memory and the agent's workspace are
  kept in a volume at `/home/node/.openclaw`.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `openclaw/openclaw`, starts a gateway that listens on
`127.0.0.1` inside its container, which no domain can reach, and that refuses
to start until a config file says `gateway.mode: local`. Cubeship runs an
image's own command, so the `Dockerfile` here is that image with a start
script, `cubeship-start.sh`. On every start it sets four keys in
`openclaw.json` — local mode, listen on all interfaces, token sign-in, and your
domain as the one allowed browser origin — then starts the gateway. Anything
else you change in the Control UI is left alone.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the Control UI answers | A domain you control, pointed at your instance. |
| The gateway token you sign in with | Nothing — the instance generates it and shows it once. **Keep a copy.** |

No model provider key is asked for. Add one in the Control UI instead: it is
written to the volume, and a key the template set would be an empty variable
on the app whenever you left it blank.

## After installing

1. Open the domain and paste the token into **Gateway secret**.
2. The page then says it is waiting for pairing approval. Keep it open: the
   start script approves the **first** browser that signs in, within a few
   seconds. It approves no other.
3. Under **Settings → Models**, add a key for a model provider — Anthropic,
   OpenAI, OpenRouter or another listed — and test the connection.
4. Talk to it in **Chat**.
5. To reach it from a chat platform, open **Channels**: paste a Telegram or
   Discord bot token, or scan WhatsApp's QR code from there. Keep each
   channel's DM policy on pairing or an allowlist, and approve senders under
   **Settings → Channels → DM access requests**. Anyone allowed to message it
   can make it run commands.

### Another browser, or a lost one

Every other browser, and a phone, is approved from **Devices** in a browser
that is already paired. If you lose the only one — cleared site data or a
private window count — add `PAIR_NEXT_BROWSER=1` to the app's variables,
redeploy, sign in from the new browser, then remove the variable and
redeploy again. While it is set, the next browser that presents the token is
approved.

## It is an agent with a shell, on the internet

Read OpenClaw's [security guide](https://docs.openclaw.ai/gateway/security)
before connecting it to anything you care about.

- **The token is full control.** Whoever has it can read every conversation,
  change the config, read the provider keys and run commands through the
  agent. It is the only thing in front of the Control UI on a public domain.
  Treat it like a root password, and rotate it by changing
  `OPENCLAW_GATEWAY_TOKEN` and redeploying if it leaks.
- **The agent runs commands inside its container**, as the unprivileged `node`
  user, with the container's network: the internet, and every other app and
  database on this instance at its internal address. Nothing here sandboxes
  it further.
- **Anything it reads can steer it.** A web page, an email or a chat message can
  carry instructions (prompt injection). Give it only the tools, channels and
  credentials a task needs, and keep chat channels on an allowlist.
- **The volume holds credentials in plain text** — provider keys, OAuth
  tokens, chat-platform sessions. Its backups do too.
- **Plugins and skills are code.** Installing one from npm or ClawHub runs it
  with the agent's access.

## What does not work here

- **Sandboxing** (`agents.defaults.sandbox`) runs tools in separate Docker
  containers and needs the Docker socket, which Cubeship does not give an app.
  Leave it off.
- **The browser tool** needs Chromium, which this image does not carry
  (OpenClaw publishes it as the much larger `-browser` variant).
- **Anything that needs a second port** is not reachable from outside: the
  domain carries only the gateway's port `18789`. Telegram's default long
  polling, Discord and WhatsApp connect outward and work; a channel's webhook
  mode on its own listener, like Telegram's on `8787`, does not.
- **Bonjour, Tailscale and host-network discovery** do not apply.
- **In-place updates** from the Control UI or `/update` cannot change the
  container; a new release of this template can.

## Updating OpenClaw

The OpenClaw version is the `FROM` line of the `Dockerfile`, at the `ref` the
template builds. OpenClaw migrates its state on start; back up the volume
first.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds: chat platforms reconnect, and a task running at that
moment is cut off. Back the volume up from the app's settings: the config,
every credential and the agent's memory are in it.

## Resources

The app is limited to 2 CPUs and 4 GiB of memory. The agent's own commands run
inside the same limits. Raise `limits` in `template.yaml` if you need more.
