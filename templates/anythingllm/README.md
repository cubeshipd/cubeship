# AnythingLLM on Cubeship

[AnythingLLM](https://anythingllm.com) is a self-hosted AI workspace: chat with
any language model, local or hosted, over documents and websites you add to
workspaces, with agents, users and an embeddable chat widget.

This template installs it on a Cubeship instance, with its data kept in a
volume.

## What it creates

- **anythingllm** — AnythingLLM `1.16.1`, built on the instance from the
  `Dockerfile` in this repository, answering on the domain you choose, with a
  volume at `/app/server/storage`: the SQLite database holding users,
  workspaces and chats, uploaded documents, the LanceDB vector store they are
  searched in, the models it downloads, and the settings file.

It needs Cubeship 0.7.0 or newer, and **an admin to install it**: the app is
built on the instance, and only admins build.

No managed database is created. The vectors are in LanceDB, in the volume, so
SQLite keeps everything in one volume and one backup.

## Why it is built

AnythingLLM writes every setting made in its UI — the model provider and its
API key, the password, the key sessions are signed with, and the key stored
credentials are encrypted with — to `/app/server/.env`. That file is outside
the storage directory, and upstream mounts it from the host on its own.
Cubeship mounts directories, not files, so on the published image
`mintplexlabs/anythingllm` every deploy would forget the model provider and
the password, and leave the instance open. The `Dockerfile` here is that image
with `/app/server/.env` turned into a link to `.env` in the volume.

For the same reason nothing secret is asked at install: AnythingLLM makes its
session and encryption keys itself, and they are kept in that file.

## What you are asked

| Input | What to give |
| --- | --- |
| Where AnythingLLM answers | A domain you control, pointed at your instance. |

## After installing

1. **Open the domain straight away.** AnythingLLM has no default account and no
   password until the setup in its UI sets one. Until you finish it, that UI —
   documents, chats and settings — is open to anyone who finds the domain.
2. The setup asks for a model provider — see below — and then who will use it:
   - *Just me* sets one password for the instance.
   - *My team* turns on multi-user mode and creates the admin account; others
     are added under *Settings → Users*.
3. Create a workspace, add documents or links to it, and chat.

The embedder is AnythingLLM's own, running inside the app, and the vectors are
kept in LanceDB in the volume; both can be changed under *Settings*.

In single-user mode the password is `AUTH_TOKEN` in the settings file. A
forgotten one is read over SSH on the machine the app runs on, since Cubeship
has no console into an app:

```bash
docker exec $(docker ps -qf name=cubeship-anythingllm-production-anythingllm) grep AUTH_TOKEN /app/server/storage/.env
```

In multi-user mode an admin resets another user's password under
*Settings → Users*.

## With Ollama and LiteLLM

Choose the provider under *Settings → AI Providers → LLM*, or in the first
setup. An app on the same instance is reachable at its internal address,
without going through a domain.

- **[Ollama](https://github.com/cubeshipd/cubeship/tree/master/templates/ollama)** runs
  models on this instance. Choose *Ollama*, and set the base URL to
  `http://cubeship-ollama-production-ollama:11434` with the Ollama template's
  suggested names. Its models are listed once it answers. Ollama can be the
  embedder too, under *Settings → AI Providers → Embedder*.
- **[LiteLLM](https://github.com/cubeshipd/cubeship/tree/master/templates/litellm)** puts
  many providers behind one OpenAI-compatible API with its own keys and spend
  limits. Choose *LiteLLM*, set the base URL to
  `http://cubeship-litellm-production-litellm:4000/v1` with the LiteLLM
  template's suggested names, and give it a LiteLLM virtual key. The models
  listed are the ones the key may use.

Change the address if you named the project, environment or app differently;
it is on the app's page in the dashboard.

## Reading links and websites

AnythingLLM reads a link, and the pages of a website, with Chromium. Upstream
runs the container with the `SYS_ADMIN` capability so Chromium can start its
own sandbox, and Cubeship gives an app no extra capability. Without it,
Chromium does not start: a link falls back to a plain download, which returns
nothing from a page drawn by JavaScript, and the website scraper finds no pages.

So the template sets `ANYTHINGLLM_CHROMIUM_ARGS` to
`--no-sandbox,--disable-setuid-sandbox`, which AnythingLLM documents for exactly
this. Chromium then works, without its sandbox: a page it opens is kept from
the rest of the machine by the container, not by Chromium. Read links you
trust, or remove the variable to turn Chromium off.

## The first start

The volume starts empty. The embedding model is downloaded from Hugging Face
into it the first time a document is embedded, and the speech-to-text model the
first time audio is transcribed. The instance needs outbound HTTPS for that,
once each.

Telemetry is off: `DISABLE_TELEMETRY` is `true`. Turning it on under *Settings →
Privacy & Data* lasts only until the next restart.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Back the volume up from the app's settings: every user,
workspace, chat, document, provider key and the password are in it. Keep the
backup as private as the keys in it.

## Updating AnythingLLM

The version is the `FROM` line of the `Dockerfile`. A new release of this
template changes it; the app is rebuilt on the next deploy, and AnythingLLM
migrates its database when it starts.

## Resources

The app is limited to 2 CPUs and 4 GiB of memory. AnythingLLM asks for 2 GB at
least; the embedding model, local speech-to-text and Chromium all run inside
this container, so large documents, many at once, or a bigger embedding model
need more — raise `limits` in `template.yaml`. The chat models run in Ollama or
behind the API you connect, not here.
