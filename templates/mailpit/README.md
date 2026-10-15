# Mailpit on Cubeship

[Mailpit](https://mailpit.axllent.org) is an email testing tool: an SMTP
server that accepts every message it is sent and keeps it, and a web inbox to
read them in, with HTML and link checks and an API.

This template installs it on a Cubeship instance as a test inbox for the apps
beside it, with SMTP on a TCP port and its messages kept in a volume.

**Nothing is delivered.** Every message sent to Mailpit stays in Mailpit,
whatever address it is for, so point staging apps at it to see the mail they
send without it reaching anyone. Mailpit can relay messages on to a real SMTP
server (`MP_SMTP_RELAY_*`); this template does not configure that.

## What it creates

- **mailpit** — Mailpit, from `axllent/mailpit:v1.31.1`, with its web inbox
  on the domain you choose behind a username and password, SMTP on port
  `1025`, and a volume at `/data` holding the message database.

It needs Cubeship 0.7.2 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the Mailpit inbox answers | A domain you control, pointed at your instance. |
| The username you sign in with | Anything without spaces or a colon. `admin` by default. |
| The password you sign in with | Nothing — the instance generates it and shows it once. **Keep a copy.** |
| How many messages to keep | `500` by default. Beyond it the oldest are deleted; `0` keeps them all. |
| The port SMTP answers on | `1025` by default. Choose a port from `1024` to `65535`, and open it in your provider's firewall too. |

## Sending mail to it

SMTP is not on the domain: a domain on Cubeship carries HTTP only. From outside
the instance, connect to its address on the port you chose. From an app on the
same instance, use Mailpit's internal address,
`cubeship-mailpit-production-mailpit` with the suggested names, on port `1025`.

It accepts any username and password, or none, over plain SMTP without TLS.
This is a test inbox, not an internet-facing mail relay; do not publish the
port to an untrusted network.
Give an app that host, port `1025`, and turn TLS off. For a
[Ghost](https://github.com/cubeshipd/cubeship/tree/master/templates/ghost) app, for
example:

```yaml
mail__transport: SMTP
mail__options__host: cubeship-mailpit-production-mailpit
mail__options__port: "1025"
mail__options__secure: "false"
```

and whatever `mail__options__auth__user` and `mail__options__auth__pass` it
already has. Then open the domain and sign in: the messages are there.

The same username and password protect Mailpit's API, at
`https://<your domain>/api/v1/`.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds, during which mail sent to it is refused. The messages
are in the volume; they are test mail, so backing it up is optional.

To change how many messages are kept, change `MP_MAX_MESSAGES` on the
`mailpit` app and redeploy. `MP_MAX_AGE` (like `7d`) deletes by age instead,
or as well.

## Resources

The app is limited to 1 CPU and 512 MiB of memory. Raise `limits` in
`template.yaml` if you keep many large messages.
