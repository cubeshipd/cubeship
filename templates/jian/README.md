# Jian on Cubeship

[Jian](https://github.com/lucasaarch/jian) is a self-hosted agent gateway that
gives AI agents a persistent identity. A profile keeps its own instructions,
model, memory and tools, and every session and channel that belongs to it —
the web panel, WhatsApp, Telegram, its API — works from that same context.

This template installs the gateway on a Cubeship instance, with its web panel
on a domain and everything it keeps in a managed PostgreSQL.

## What it creates

- **jian-web** — the panel, Jian `1.1.3` from `ghcr.io/lucasaarch/jian-web`,
  on the domain you choose. It is the only public door: it passes `/v1`,
  `/health`, `/openapi.json` and `/jian-remote.mjs` on to the gateway over
  the instance's private network, so the browser, channel webhooks, OAuth
  callbacks and remote workers all use the one domain.
- **jian** — the gateway, Jian `1.1.3` from `ghcr.io/lucasaarch/jian-gateway`:
  the API, the agents and the workers. It has no domain of its own; the web
  app reaches it at its private address, in `JIAN_GATEWAY_URL`. A volume at
  `/home/node` keeps the agents' workbench; everything else lives in the
  database, so a redeploy loses nothing.
- **jian-db** — PostgreSQL 17 with a database named `jian`, attached to the
  gateway. Profiles, sessions, history, memories, the run queue and the
  encrypted vault are all in it. Jian migrates it on start.

It needs Cubeship 0.14.0-rc.1 or newer, for explicit per-port health checks
and the gateway's volume.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the gateway and its panel answer | A domain you control, pointed at your instance. |
| The token you sign in with | Nothing — the instance generates it and shows it once. **Keep a copy.** |
| The key the vault encrypts credentials with | Nothing — the instance generates it and shows it once. **Keep a copy, away from the database backups.** |

No model provider key is asked for. Add one in the panel instead: it is
encrypted into the vault, and a key the template set would be an empty
variable on the app whenever you left it blank.

## After installing

1. Open `https://<your domain>/` and sign in with the token.
2. Under **Providers**, add a key for Anthropic, Gemini or OpenAI, or sign in
   with ChatGPT.
3. Create a profile and talk to it.
4. To reach it from a chat platform, open **Channels** on the profile.
   WhatsApp pairs by scanning a QR code. Telegram takes a BotFather token and
   points the bot at this domain by itself. A stranger's first message
   becomes a contact request you approve.

## Agents that run commands

A profile with **Shell** turned on runs commands in the gateway's container,
which carries Node, Python with `uv`, Go, `git`, `gh`, `ssh`, `curl`, `wget`,
the text tools (`sed`, `awk`, `rg`, `jq`) and a C toolchain. The agent knows
the list from a built-in skill and installs more when a task needs it.

The agent installs as an ordinary user, never as root — `npm install -g`,
`uv tool install`, `go install`, a binary in `~/.local/bin` — and all of it
lands on the `/home/node` volume, with its SSH keys and its Git and `gh`
logins. It
survives redeploys and updates, and it is in the volume's backups: a key or a
login stored there is in them too.

To let an agent search the internet, add a Tavily key under **Providers →
Web search** and switch on **Allow web search** on its profile. The free
Tavily plan covers 1,000 searches a month.

In a group, the agent reads the conversation and answers only when it is
mentioned or replied to. A Telegram bot reads a group only with privacy mode
off: in BotFather, `/setprivacy` → **Disable**, then add the bot to the group
again.

## The token is the whole installation

Jian has one owner and one credential. Whoever has the token can read every
conversation, change every profile and run anything the agents can. It is the
only thing in front of the panel on a public domain. Treat it like a root
password. To rotate it, change `JIAN_API_TOKEN` on the app and redeploy: that
also signs every panel session out.

## The vault key

Provider keys, MCP tokens and channel credentials are encrypted with the key
in `JIAN_MASTER_KEYS` and never read back. The key lives on the app, not in
the database, so **a database backup without it cannot open a single stored
credential**. Keep the generated value somewhere of its own.

To rotate it, add a second entry and make it the active one — keep the old
one until every credential has been sent again from the screen that
configures it:

```text
JIAN_ACTIVE_KEY_ID=v2
JIAN_MASTER_KEYS={"v1":"<old key>=","v2":"<44 base64 characters for 32 new bytes>"}
```

## What Jian can reach

Outbound calls go to public HTTPS only. Private addresses — including every
other app and database on this instance — are refused unless named, exactly,
in `JIAN_ALLOW_PRIVATE_ORIGINS`: for an Ollama on the same instance,
`http://cubeship-<project>-<environment>-ollama:11434`. Metadata and
link-local addresses stay blocked whatever it says.

## What does not work here

- **More than one copy of the gateway.** Jian migrates on start and copies
  would race for it, and an app with a volume runs as one copy on one server
  anyway.

The gateway believes the client address the web app forwards only from
private addresses (`JIAN_TRUSTED_PROXIES=uniquelocal`), so rate limits count
per client. Never give the gateway a public domain while that is set.

## Updating Jian

The Jian version is the image `tag` of both apps in `template.yaml`; the web
app and the gateway always run the same version. Jian migrates its database on
start and a migration does not undo itself: back the database up before
changing the installed apps to a newer image. Going back to an older image
does not go back on the schema.

From a version before 1.0.0, the gateway no longer serves the panel, and
answers `/ui/` with a page explaining the move. Add a `jian-web` app with this
template's settings and `JIAN_GATEWAY_URL` set to
`http://cubeship-<project>-<environment>-jian:4310`, move the domain from the
gateway to it, and set `JIAN_TRUSTED_PROXIES=uniquelocal` on the gateway.
`JIAN_PUBLIC_URL` stays the same, and webhook, OAuth and worker addresses on
that domain keep working. Open the panel at the domain's root: old `/ui/...`
bookmarks lead nowhere.

After updating, check each server under **MCP**. It can use a header, a local
command or an OAuth sign-in, depending on its transport and authentication.

## Resources

The gateway is limited to 1 CPU and 1 GiB of memory, the database to the
same, and the web app to half a CPU and 512 MiB. A Go or C build that an
agent runs counts against the gateway's share. Raise `limits` in
`template.yaml` if you need more.

---

<!-- cubeship-crosslink -->

## About Cubeship

This is a template for [**Cubeship**](https://github.com/cubeshipd/cubeship) —
a PaaS you run on your own server: `docker push`, and it is live, with HTTPS,
a database beside it, and a second machine when one stops being enough.

Browse every template at [cubeship.dev/templates](https://cubeship.dev/templates).
