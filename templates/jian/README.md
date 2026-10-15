# Jian on Cubeship

[Jian](https://github.com/lucasaarch/jian) is a self-hosted agent gateway that
gives AI agents a persistent identity. A profile keeps its own instructions,
model, memory and tools, and every session and channel that belongs to it —
the web panel, WhatsApp, Telegram, its API — works from that same context.

This template installs the gateway on a Cubeship instance, with its web panel
on a domain and everything it keeps in a managed PostgreSQL.

## What it creates

- **jian** — Jian `0.1.0`, from `ghcr.io/lucasaarch/jian-gateway`. The API
  answers on the domain you choose, and the panel under `/ui/` on the same
  domain. A volume at `/home/node` keeps the agents' workbench; everything
  else lives in the database, so a redeploy loses nothing.
- **jian-db** — PostgreSQL 17 with a database named `jian`, attached to the
  gateway. Profiles, sessions, history, memories, the run queue and the
  encrypted vault are all in it. Jian migrates it on start.

It needs Cubeship 0.7.0 or newer, for the volume.

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

1. Open `https://<your domain>/ui/` and sign in with the token.
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

- **Per-client rate limiting.** Jian limits requests by connection address and
  does not trust `X-Forwarded-For`, so behind the instance's proxy every
  client shares one limit.
- **More than one copy.** Jian migrates on start and copies would race for
  it, and an app with a volume runs as one copy on one server anyway.

## Updating Jian

The Jian version is the image `tag` in `template.yaml`. Jian migrates its
database on start and a migration does not undo itself: back the database up
before changing the installed app to a newer image. Going back to an older
image does not go back on the schema.

After updating, check each server under **MCP**. It can use a header, a local
command or an OAuth sign-in, depending on its transport and authentication.

## Resources

The gateway is limited to 1 CPU and 1 GiB of memory, and the database to the
same. A Go or C build that an agent runs counts against the gateway's
share. Raise `limits` in `template.yaml` if you need more.

---

<!-- cubeship-crosslink -->

## About Cubeship

This is a template for [**Cubeship**](https://github.com/cubeshipd/cubeship) —
a PaaS you run on your own server: `docker push`, and it is live, with HTTPS,
a database beside it, and a second machine when one stops being enough.

Browse every template at [cubeship.dev/templates](https://cubeship.dev/templates).
