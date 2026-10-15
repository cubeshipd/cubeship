# OpenHands on Cubeship

[OpenHands](https://github.com/OpenHands/OpenHands) is an open-source AI
software engineer. Its web app, Agent Canvas, runs coding agents that read and
write code, run commands and browse, and automations that run them on a
schedule or from a webhook. It uses any model provider you give it a key for.

This template installs it on a Cubeship instance, with the web app on a domain
and everything it keeps in two volumes.

## What it creates

- **openhands** — Agent Canvas `1.18.0`, built on the instance from the
  `Dockerfile` in this repository: the web app, the agent server and the
  automation backend in one container, answering on the domain you choose.
  Settings, secrets, conversations and automations are kept in a volume at
  `/home/openhands/.openhands`, and the code the agent works on in one at
  `/projects`.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `ghcr.io/openhands/agent-canvas`, writes its API key into
the page it serves, so the browser is signed in without asking. That is meant
for a machine only you can reach; on a domain it would sign in anyone who opens
it. The `Dockerfile` here is that image with one flag changed, the one
OpenHands' own `--public` mode changes: the page asks for the key instead. The
build fails rather than ship the old behaviour if a new image stops matching.

## What you are asked

| Input | What to give |
| --- | --- |
| Where OpenHands answers | A domain you control, pointed at your instance. |
| The API key you sign in with | Nothing — the instance generates it and shows it once. **Keep a copy.** |

No model provider key is asked for. Add one in the app instead: it is stored,
encrypted, in the volume.

## After installing

1. Open the domain. It sends you to `/canvas`, which asks for the API key:
   paste the generated one.
2. Under *Settings*, choose a model provider and enter its key.
3. Start a conversation. The agent works in `/projects`; clone a repository
   there, or add a GitHub token under *Settings* and let it clone one.

## It is on the internet

The API key is all that stands between the domain and an agent that runs any
command in its container, with the model keys and tokens you gave it. OpenHands
documents a firewall or an IP allow-list in front of it as well; Cubeship
cannot restrict a domain by address, so keep the key long and to yourself.

The browser keeps the key in the page's storage. The built-in editor, at
`/vscode` on the same domain, can read that storage, which OpenHands tracks as
[#16492](https://github.com/OpenHands/OpenHands/issues/16492).

To reach this instance from an Agent Canvas on your own machine, add it under
*Manage backends* with the domain as its host and the API key.

## What the agent can reach

The agent runs its commands inside the container, as the unprivileged
`openhands` user: the two volumes, the internet, and other apps on the instance
at their internal addresses. It has no Docker socket and no privileged mode, so
it cannot start containers, and OpenHands' Docker sandbox, which would give
each conversation a container of its own, does not work here.

## The volumes

The app runs as one copy on the machine its volumes are on, and a deploy stops
it for a few seconds, ending any conversation in progress. Back both up from
the app's settings. The key that encrypts stored secrets is generated on first
start and kept in the first volume, so a backup of it restores them.

## Telemetry

Agent Canvas sends anonymous usage events to OpenHands unless told otherwise.
Set `AGENT_CANVAS_DISABLE_TELEMETRY=1` on the app and redeploy to turn off the
web app's.

## Updating

Change the tag in the `Dockerfile`, release this repository, and point `ref` at
the new release.

## Resources

The app is limited to 2 CPUs and 4 GiB of memory. The agent builds and tests
code in the same container, so raise `limits` in `template.yaml` for large
projects.
