# Ackee on Cubeship

[Ackee](https://ackee.electerious.com) is a self-hosted analytics tool that
cares about privacy: no cookies, no unique user tracking, and the data stays on
your own server.

This template installs it on a Cubeship instance with the managed MongoDB it
needs.

## What it creates

- **ackee** — the dashboard and tracking endpoint, from
  `electerious/ackee:3.6.0`, answering on the domain you choose.
- **ackee-db** — a managed MongoDB 8.0 database, attached to the app.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the dashboard answers | A domain you control, pointed at your instance. |
| The username you sign in with | Anything; `admin` unless you change it. |
| The password you sign in with | Nothing — the instance generates it and shows it once. **Keep a copy.** |
| The sites allowed to send visits | Every origin the tracker runs on, comma-separated: `https://example.com,https://www.example.com`. |

## After installing

1. Open the domain and sign in with the username and the generated password.
2. Under *Settings → Domains*, add each site and copy its embed code.
3. Paste it into the site:

   ```html
   <script async src="https://<your domain>/tracker.js" data-ackee-server="https://<your domain>" data-ackee-domain-id="<domain id>"></script>
   ```

A site missing from the allowed origins sends nothing: the browser refuses the
request. To add one later, change `ACKEE_ALLOW_ORIGIN` on the `ackee` app and
redeploy. The username and password are variables on the same app.

## Resources

The app is limited to 0.5 CPU and 512 MiB of memory. Raise `limits` in
`template.yaml` if your traffic needs more.
