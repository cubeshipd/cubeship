# OpenCode on Cubeship

[OpenCode](https://opencode.ai) is an open-source AI coding agent: it reads and
edits code, runs shell commands, and works with models from Anthropic, OpenAI,
Google, OpenRouter and many other providers.

This template installs its server on a Cubeship instance, with the web UI on a
domain, its sessions and settings in one volume and the code it works on in
another.

> **This puts a coding agent that runs shell commands on the internet.** Anyone
> with the password can make it run anything inside its container, read every
> file in its volumes, use your provider keys, and reach the other apps on the
> instance. Read [Its server is on the internet](#its-server-is-on-the-internet)
> before installing.

## What it creates

- **opencode** — OpenCode `1.18.30`, built on the instance from the
  `Dockerfile` in this repository. The web UI and the server's API answer on
  the domain you choose, behind a password. Two volumes:
  - `/root` — its config (`.config/opencode`), provider keys and sessions
    (`.local/share/opencode`), and whatever else you keep in the home
    directory, like `.ssh` and `.gitconfig`.
  - `/workspace` — the code it works on.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

## Why it is built

The published image, `ghcr.io/anomalyco/opencode`, runs `opencode`, which
starts the terminal UI and cannot run without a terminal. Cubeship runs an
image's own command, so the `Dockerfile` here is that image running
`opencode serve` on port `4096`, with `git` and an SSH client added and
`/workspace` as its working directory. The server is the same one the terminal
UI talks to, and it serves the web UI too.

## What you are asked

| Input | What to give |
| --- | --- |
| Where OpenCode answers | A domain you control, pointed at your instance. |
| The password you sign in with | Nothing — the instance generates it and shows it once. **Keep a copy.** |

The username is `opencode`. Set `OPENCODE_SERVER_USERNAME` on the app to change
it.

No model provider key is asked for. Add one in OpenCode instead: it is written
to the volume, and a key the template set would be an empty variable on the app
whenever you left it blank.

## After installing

1. Open the domain and sign in as `opencode` with the generated password.
2. Connect a provider: run `/connect` and choose one. Keys are kept in
   `/root/.local/share/opencode/auth.json`.
   A provider's usual variable, like `ANTHROPIC_API_KEY`, set on the app works
   too.
3. Give it code. Open a terminal in the web UI, or ask OpenCode to run the
   commands itself — Cubeship has no console into an app — and clone into
   `/workspace`:

   ```bash
   git config --global user.name "Your Name"
   git config --global user.email you@example.com
   cd /workspace && git clone https://github.com/you/project.git
   ```

   For private repositories, create a key with `ssh-keygen -t ed25519` and add
   `/root/.ssh/id_ed25519.pub` to the repository as a deploy key. Both survive
   deploys: they are in the `/root` volume.
4. Open the project's directory in the web UI and start a session.

### From your own terminal

The terminal UI on your laptop can use this server instead of its own:

```bash
OPENCODE_SERVER_PASSWORD=<the password> opencode attach https://<your domain>
```

The code, the shell commands and the keys stay on the instance.

## Its server is on the internet

OpenCode's server has one account and one password, sent as HTTP basic auth,
and no rate limit on guessing it. Behind that password is a shell: OpenCode
runs its commands as root inside the container, with no sandbox beyond the
container itself. It has no Docker socket and cannot reach the host, but it can
reach the internet and every other app on the instance at its internal
address.

- Keep the generated password, or use a longer one. Never an easy one.
- Keep the domain to yourself, and give the password to nobody you would not
  give a shell to.
- Keep only the credentials it needs on it: a deploy key per repository rather
  than your own SSH key, and a provider key with a spending limit.
- Remember what it can reach. An agent told to run something by a file it read
  — a README, an issue, a web page — will try to.
- Uninstall it when you stop using it.

OpenCode's permission settings, which decide what it asks before doing, are a
guard against mistakes, not against someone who has the password: whoever signs
in can change them.

## Updating OpenCode

The OpenCode version is the `FROM` line of the `Dockerfile`, at the `ref` the
template builds. OpenCode's own upgrade cannot replace the binary in a built
image; a new release of this template can.

## The volumes

The app runs as one copy on the machine its volumes are on, and a deploy stops
it for a few seconds: a session running at that moment is cut off. Back both
volumes up from the app's settings — the provider keys are in `/root`, and work
not pushed anywhere is only in `/workspace`.

## Resources

The app is limited to 2 CPUs and 4 GiB of memory. Language servers, builds and
tests run inside it too: raise `limits` in `template.yaml` for large projects.
