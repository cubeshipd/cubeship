# Ollama on Cubeship

[Ollama](https://ollama.com) runs open language models — Llama, Qwen, Gemma
and others — on your own machine, behind an HTTP API that chat front-ends,
automation tools and code can call.

This template installs it on a Cubeship instance as an internal service, with
the models it downloads kept in a volume.

## What it creates

- **ollama** — Ollama, from `ollama/ollama:0.34.0`, with its API on port
  `11434` at an internal address only, and a volume at `/root/.ollama`, where
  the models you pull are kept.

It needs Cubeship 0.7.0 or newer.

## What you are asked

Nothing.

## It has no domain, on purpose

Ollama's API has no authentication. On a public domain, anyone who found it
could run models on your machine's CPU and pull any model onto its disk. So
the app answers only to other apps on the same instance, at

```
http://cubeship-ollama-production-ollama:11434
```

with the suggested names — the address is on the app's page in the dashboard.
Put an app with its own sign-in in front of it:

- **Open WebUI** — set `OLLAMA_BASE_URL` to the address above.
- **n8n** — create an *Ollama* credential with the address above as the base
  URL.
- **Your own code** — Ollama's API at `/api/…`, or the OpenAI-compatible one at
  `/v1/…`, which takes any API key and checks none.

## CPU only

Cubeship gives an app no GPU, so every model runs on the processor, from
system memory. That works, and it is slow next to a GPU: a 3–4B model on a
4-core VPS writes a few words a second, and takes longer still before the
first word when the prompt is long. A 7–8B model roughly halves that. Models
of 14B and up are not practical here.

Choose small models. These fit in the app's 8 GiB with room for the context:

| Model | Download | Good for |
| --- | --- | --- |
| `llama3.2:1b` | 1.3 GB | The fastest answers, short and simple tasks |
| `qwen3:1.7b` | 1.4 GB | Fast, with reasoning |
| `llama3.2` (3B) | 2.0 GB | A good default for chat and summaries |
| `qwen3:4b` | 2.5 GB | Better answers, slower |
| `gemma3` (4B) | 3.3 GB | Better answers, slower |
| `nomic-embed-text` | 274 MB | Embeddings, for search and RAG |

A model needs about its download size in memory, plus the context.

## Pulling a model

The app starts with no models, and Cubeship has no shell into an app, so a
model is pulled through the API, from an app on the same instance:

- **From Open WebUI:** *Settings → Admin → Connections*, *Manage* on the
  Ollama connection, and type a model name to download. Or type a name in the
  model selector, and it offers to pull it.
- **From any app that can send an HTTP request** — an n8n *HTTP Request* node,
  for instance — `POST` to `/api/pull`:

  ```json
  { "model": "llama3.2" }
  ```

A model is downloaded once. It stays in the volume across deploys and
restarts.

## Choices this template makes

- **One model in memory at a time.** `OLLAMA_MAX_LOADED_MODELS` is `1`. Asking
  for another model unloads the first, which takes a few seconds either way. A
  model is unloaded after five minutes without requests; set
  `OLLAMA_KEEP_ALIVE` (`30m`, or `-1` for never) to keep it loaded longer.
- **A 4,096-token context.** `OLLAMA_CONTEXT_LENGTH` is `4096`. Agents,
  coding tools and long documents want more — raise it, and raise the memory
  limit with it.
- **One request at a time.** Ollama's default `OLLAMA_NUM_PARALLEL` of `1`
  stays: on a CPU, two requests at once are each slower than two in turn, and
  each costs its own context in memory.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Models take gigabytes of disk each; delete the ones you
no longer use, from Open WebUI or with `DELETE /api/delete`. Backing the
volume up copies every model — they can be pulled again instead.

## Resources

The app is limited to 4 CPUs and 8 GiB of memory, and Ollama sizes what it
loads against that limit, not the machine's. For a 7–8B model with a longer context, raise
`limits` in `template.yaml` to 12 or 16 GiB, if the machine has it. More CPUs
make answers faster, up to the machine's physical cores.
