# Open WebUI on Cubeship

[Open WebUI](https://openwebui.com) is a self-hosted chat interface for
language models: it talks to Ollama and to any OpenAI-compatible API, keeps
chats and users, and answers questions over documents you upload.

This template installs it on a Cubeship instance, with its data kept in a
volume.

## What it creates

- **open-webui** — Open WebUI, from `ghcr.io/open-webui/open-webui:v0.11.3`,
  answering on the domain you choose, with a volume at `/app/backend/data`:
  the SQLite database holding users, chats and settings, uploaded files, the
  Chroma vector store documents are searched in, and the embedding model.

It needs Cubeship 0.7.0 or newer.

No managed database is created. Open WebUI can keep its data in Postgres, but
its vectors would still be in Chroma, in the volume — the managed Postgres has
no pgvector — so chats in the database and documents in the volume would be
backed up separately and could come back out of step. SQLite keeps all of it in
one volume and one backup.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Open WebUI answers | A domain you control, pointed at your instance. |
| The key sessions are signed with | Nothing — the instance generates it and shows it once. |
| Your Ollama server | The Ollama app's internal address. Pre-filled with the [Ollama template](https://github.com/cubeshipd/cubeship/tree/master/templates/ollama)'s, with its suggested names. Leave it empty if you have no Ollama. |

An address that does not answer breaks nothing: Open WebUI logs a connection
error and lists no Ollama models.

## After installing

1. **Open the domain straight away.** Open WebUI has no default account: the
   first person to sign up becomes the admin, and sign-ups are then turned off.
   Until you do, that is anyone who finds the domain.
2. To let others in, turn *Enable New Sign Ups* on under *Admin Settings →
   General*. New accounts wait as *pending* until an admin approves them.
3. Add models under *Admin Settings → Connections*: Ollama, and any
   OpenAI-compatible API with its key.

The Ollama address and `WEBUI_URL` are read on the first start only. After
that, Open WebUI keeps its settings in its database, and changing the
variables changes nothing — change them in *Admin Settings* instead.

## With Ollama and LiteLLM

- **[Ollama](https://github.com/cubeshipd/cubeship/tree/master/templates/ollama)** runs
  models on this instance, with no domain and no authentication of its own.
  Open WebUI is the sign-in in front of it: install Ollama first, keep the
  pre-filled address, and pull models from *Admin Settings → Connections →
  Manage*, or by typing a model's name in the model selector.
- **LiteLLM** puts many providers — OpenAI, Anthropic, Gemini, Bedrock — behind
  one OpenAI-compatible API with its own keys and spend limits. Add it under
  *Admin Settings → Connections → OpenAI API*, with its internal address
  followed by `/v1` (`http://cubeship-<project>-<environment>-<app>:4000/v1`)
  and a LiteLLM virtual key.

## The first start

The image carries the embedding and speech-to-text models under
`/app/backend/data/cache`, and the volume mounted there starts empty. So the
first start downloads the embedding model from Hugging Face into the volume,
about 100 MB, and speech-to-text downloads its own on first use. The instance
needs outbound HTTPS for that, once. Until the download finishes, documents
cannot be searched.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Back the volume up from the app's settings: every user,
chat, uploaded file and API key is in it.

## Resources

The app is limited to 2 CPUs and 2 GiB of memory. The embedding model runs
inside Open WebUI, so embedding large documents is slow on 2 CPUs; a larger
embedding model, a reranker, or local speech-to-text needs more memory — raise
`limits` in `template.yaml`. The chat models themselves run in Ollama or behind
the API you connect, not here.
