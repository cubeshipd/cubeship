# Flowise on Cubeship

[Flowise](https://flowiseai.com) is a visual builder for LLM apps: chatflows,
agents and RAG pipelines assembled from nodes in the browser, then served as a
chat widget or an API.

This template installs it on a Cubeship instance with the managed Postgres it
keeps flows, credentials and users in, and a volume for its files.

## What it creates

- **flowise** — Flowise, from `flowiseai/flowise:3.1.4`, answering on the
  domain you choose, with a volume at `/home/node/.flowise`: uploaded files,
  local vector stores, and the secrets Flowise signs sessions with.
- **flowise-db** — a managed Postgres 18 database, attached to the app. Flowise
  creates its tables on first start.

It needs Cubeship 0.7.0 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Flowise answers | A domain you control, pointed at your instance. |
| The key Flowise encrypts stored credentials with | Nothing — the instance generates it and shows it once. **Keep a copy, and never change it.** |

### The encryption key must never change

The API keys you save under *Credentials* are encrypted with
`FLOWISE_SECRETKEY_OVERWRITE` before they are written to the database. Change
it, or lose it and set a new one, and every stored credential becomes
unreadable: each has to be entered again. Because the key comes from the
template rather than a file in the volume, the database and this key are
enough to bring the credentials back. Keep it wherever you keep the database's
backups.

## After installing

1. **Open the domain straight away.** Flowise has no default account: the
   first person to register becomes the owner, and every later registration is
   refused. Until you do, that is anyone who finds the domain.
2. Under *Credentials*, add the keys for the model providers you use.
3. Build a chatflow or an agentflow, and give it an API key under *API Keys*
   before calling it from another app.

The open-source edition has that one account. Inviting more users needs
workspaces, which are part of Flowise's paid plans.

`FLOWISE_USERNAME` and `FLOWISE_PASSWORD`, the sign-in of Flowise 2, do nothing
here.

The URLs assume the instance serves HTTPS, and the session cookie is marked
Secure because `APP_URL` starts with `https://`. On an instance with TLS off,
change `APP_URL` to start with `http://`, or signing in will not stick.

A forgotten password is reset over SSH on the machine the app runs on, since
Cubeship has no console into an app:

```bash
docker exec $(docker ps -qf name=cubeship-flowise-production-flowise) \
  flowise user --email you@example.com --password 'the new password'
```

Without `--email` and `--password`, the command lists the accounts.

## With Ollama and LiteLLM

Apps on the same instance reach each other at their internal addresses,
without going through a domain. With the suggested names:

- **[Ollama](https://github.com/cubeshipd/cubeship/tree/master/templates/ollama)** — in the
  *Ollama* chat model and *Ollama Embedding* nodes, set *Base URL* to
  `http://cubeship-ollama-production-ollama:11434`, and the model name to one
  you have pulled. Ollama has no authentication; nothing outside the instance
  can reach it.
- **[LiteLLM](https://github.com/cubeshipd/cubeship/tree/master/templates/litellm)** — use
  the *OpenAI Custom Model* chat model node (or *OpenAI*) with an OpenAI
  credential holding a LiteLLM virtual key, and under *Additional Parameters*
  set *Base Path* to `http://cubeship-litellm-production-litellm:4000/v1`. The
  model name is one you added under *Models* in LiteLLM.

The addresses are on each app's page in the dashboard.

## It is on the internet

The domain serves the editor and the API. The editor asks for the account's
email and password. A chatflow's prediction endpoint answers anyone who has
its ID unless the flow has an API key assigned, and a flow shared as a public
chatbot answers everyone: assign keys before sharing a flow's URL.

Flowise sends no telemetry unless `POSTHOG_PUBLIC_API_KEY` is set, and the
image does not set it.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Flows, credentials and users are in the database, and
uploaded files and local vector stores are in the volume: back up both, from
the database's and the app's settings.

Losing only the volume loses the uploads and signs everyone out; the
credentials stay readable, because their key is the one you were shown.

Flowise's own log files are written inside the image and do not survive a
deploy. What it prints is in the app's logs in the dashboard.

## Resources

The app is limited to 1 CPU and 2 GiB of memory. The models run in Ollama or
behind the API you connect, not here; large document loaders, the headless
browser scrapers and in-memory vector stores are what need more — raise
`limits` in `template.yaml`.
