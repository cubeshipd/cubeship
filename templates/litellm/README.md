# LiteLLM on Cubeship

[LiteLLM](https://github.com/BerriAI/litellm) is an open-source LLM gateway:
one OpenAI-compatible API in front of OpenAI, Anthropic, Gemini, Bedrock,
OpenRouter, Ollama and a hundred other providers, with virtual keys, budgets,
rate limits and spend tracking per key, team and user.

This template installs the LiteLLM proxy on a Cubeship instance with the
managed Postgres it keeps its models, keys and spend in.

## What it creates

- **litellm** — the LiteLLM proxy, from `ghcr.io/berriai/litellm:v1.100.1`.
  The API and the admin UI (at `/ui`) answer on the domain you choose, on
  port `4000` inside the instance.
- **litellm-db** — a managed Postgres 18 database, attached to the app, so
  `DATABASE_URL` is set for you. The proxy creates its tables on first start.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the proxy and its admin UI answer | A domain you control, pointed at your instance. |
| The master key, without its sk- prefix | Nothing — the instance generates it and shows it once. **Keep a copy.** |
| The key provider credentials are encrypted with | Nothing — the instance generates it and shows it once. **Keep a copy, and never change it.** |
| The username you sign in to the admin UI with | Anything; `admin` unless you change it. |
| The password you sign in to the admin UI with | Nothing — the instance generates it and shows it once. |

### The two keys start with `sk-`

LiteLLM expects its master key to start with `sk-`, and the instance
generates letters and digits only, so the template sets
`LITELLM_MASTER_KEY` to `sk-` followed by the generated value. When you use
the master key, **type the `sk-` in front of what the instance showed you**.
`LITELLM_SALT_KEY` is composed the same way.

### The salt key must never change

`LITELLM_SALT_KEY` encrypts the provider API keys you add in the admin UI
before they are written to the database. Change it, or lose it and set a new
one, and every stored provider key becomes unreadable: each model has to be
added again. Keep it wherever you keep the database's backups.

No model provider key is asked for. Add them in the admin UI instead: they are
stored in the database, encrypted.

## After installing

1. Open `https://<your domain>/ui` and sign in with the username and the
   generated password.
2. Under *Models*, add a model: choose the provider, the model name and its
   API key, and test the connection.
3. Under *Virtual Keys*, create a key for each app or person that will call
   the proxy, and choose which models it may use. Give that key out, never
   the master key.

## Pointing other apps at it

Apps on the same instance reach the proxy at its internal address, without
going through the domain:

```
http://cubeship-litellm-production-litellm:4000
```

with the suggested names — the address is on the app's page in the dashboard.
Use a virtual key as the API key, and a model name exactly as you added it
under *Models*.

- **OpenCode** — add an OpenAI-compatible provider to `opencode.json` (in
  `/root/.config/opencode`), with `"npm": "@ai-sdk/openai-compatible"`,
  `options.baseURL` set to the address above followed by `/v1`, and
  `options.apiKey` set to the virtual key; list the models under `models`.
- **OpenHands** — under *Settings*, open the advanced options, set the model
  to `litellm_proxy/<model name>`, the base URL to the address above, and
  the API key to the virtual key.
- **OpenClaw** — under **Settings → Models**, add an OpenAI-compatible
  provider with the address above followed by `/v1` as its base URL and the
  virtual key as its API key.
- **Open WebUI** — under *Admin Settings → Connections*, add an OpenAI API
  connection with the address above followed by `/v1` as its URL and the
  virtual key as its key. Its model list is the models the key may use.
- **Your own code** — any OpenAI SDK, with the base URL
  `http://cubeship-litellm-production-litellm:4000/v1` inside the instance,
  or `https://<your domain>/v1` from anywhere else.

## It is on the internet

The domain serves the API and the admin UI. Every API route asks for a
virtual key or the master key, and the UI asks for its username and password;
only the health checks and a few public pages answer without one. The master
key can create keys, read every key's spend and change every setting: keep it
out of apps, and give each one a virtual key with a budget instead.

## Resources

The app is limited to 1 CPU and 4 GiB of memory, what LiteLLM recommends for
one proxy process. The proxy only forwards requests, so it needs no GPU and
no more CPU for bigger models; raise `limits` in `template.yaml` if many
requests arrive at once.
