# Grok Build on Cubeship

[Grok Build](https://github.com/xai-org/grok-build) is SpaceXAI's open-source
coding agent: it reads a codebase, edits files, runs shell commands, and
manages git. It is mostly used as a terminal UI, and it can also run as a
long-lived server that programs talk to over the
[Agent Client Protocol](https://agentclientprotocol.com) (ACP).

This template installs that server, `grok agent serve`, on a Cubeship
instance: a WebSocket endpoint on a domain, behind a token, with its sessions
and your repositories in a volume.

> **There is no web page at the domain.** What answers is a WebSocket speaking
> ACP JSON-RPC. You need a client that speaks ACP over WebSocket: your own
> program built with an ACP SDK, or a third-party interface that supports a
> remote `grok agent serve`.

## What it creates

- **grok** — Grok Build `1.0.30`, built on the instance from the `Dockerfile`
  in this repository. The agent server answers at `wss://<your domain>/ws`;
  sessions, memory, config and the repositories it works in are kept in a
  volume at `/home/grok`.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

SpaceXAI publishes no container image for Grok Build, only a binary per
platform through its installer. The `Dockerfile` here downloads the Linux
binary of one pinned release from `x.ai/cli`, checks it against a SHA-256
recorded in the file, installs `git`, `ripgrep` and `ssh` for the agent's
tools, and runs `grok agent serve --bind 0.0.0.0:2419` as an unprivileged
user.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the agent server answers | A domain you control, pointed at your instance. |
| The token clients connect with | Nothing — the instance generates it and shows it once. **Keep a copy.** It becomes `GROK_AGENT_SECRET`. |
| Your xAI API key | A key from [console.x.ai](https://console.x.ai). Usage is billed to that account. |

The API key is asked for at install because the server has no screen to enter
one on, and its browser sign-in needs a terminal inside the container. To
change it later, change `XAI_API_KEY` in the app's environment and redeploy.

## Connecting

Open a WebSocket to `wss://<your domain>/ws` with the token in a header:

```
Authorization: Bearer <token>
```

The server also accepts `?server-key=<token>` in the URL. Prefer the header:
a URL ends up in logs and browser history.

Then speak ACP: `initialize`, `session/new` with a `cwd`, `session/prompt`.
The `cwd` is a directory **inside the container**, and it must exist — use a
path under `/home/grok`, which is the volume. Ask the agent to clone a
repository there, or create the directory first. Reconnecting keeps the
agent's state, and `session/load` resumes a session by ID.

Grok's documented editor integrations (Zed, Neovim, Emacs) start
`grok agent stdio` as a local process; they do not connect to this server
directly. The [agent mode guide](https://github.com/xai-org/grok-build/blob/main/crates/codegen/xai-grok-pager/docs/user-guide/15-agent-mode.md)
lists the ACP SDKs.

## A shell-capable agent on the internet

**Whoever holds the token can run any command this container can**, read and
write every repository in the volume, spend your xAI credit, and reach the
internet and every other app on the instance at its internal address. The
token is the only thing in front of it.

- Keep the token secret, and do not share the domain with anyone you would not
  give a shell to.
- The server runs in Grok's default *ask* mode: read-only tools and read-only
  shell commands run on their own, and anything else asks the client to
  approve it. A client can turn that off for its own session
  (`_meta.yoloMode`), so the prompt protects you from the model, not from
  someone holding the token.
- Put credentials in the volume (an SSH key, a GitHub token) only if you are
  prepared for the agent to use them.
- To rotate the token, change `GROK_AGENT_SECRET` and redeploy.

The agent runs as an unprivileged user with no Docker socket. Its sandbox
profiles, which rely on OS isolation, are not set up here.

## What leaves the instance

Grok Build sends prompts, and the files and command output the agent reads, to
xAI's API. A third party has
[reported](https://github.com/cereblab/grok-build-exfil-repro) that the CLI also
uploads repository contents and git history to xAI; this template does not
verify or prevent that. Read xAI's terms before pointing it at code you may not
share.

## Updating Grok Build

The version and its checksums are in the `Dockerfile`, at the `ref` the
template builds. `grok update` cannot replace the binary in place: it is
owned by root and rebuilt with the image. A new release of this template can
update it.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds: connected clients drop, and a turn running at that
moment is cut off. Everything the agent keeps — `~/.grok` and your
repositories — is in `/home/grok`; back it up from the app's settings.

## License

Grok Build's first-party code is under the Apache License 2.0; the binary is
SpaceXAI's official build of it. This template does not redistribute it: the
instance downloads it from `x.ai` when it builds.

## Resources

The app is limited to 2 CPUs and 4 GiB of memory. Builds and tests the agent
runs share that; raise `limits` in `template.yaml` if you need more.
