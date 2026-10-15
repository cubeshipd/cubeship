# Jellyfin on Cubeship

[Jellyfin](https://jellyfin.org) is a free, self-hosted media server: it
organises your films, series, music and books, fetches their artwork and
metadata, and streams them to a browser and to its apps for phones, TVs and
desktops.

This template installs it on a Cubeship instance, with volumes for its
configuration, its cache and your media.

## What it creates

- **jellyfin** — Jellyfin 12.0, from `jellyfin/jellyfin:12.0`, answering on
  the domain you choose, with three volumes:
  - `/config` — the database with users, libraries and watch history, plus
    settings, plugins and logs.
  - `/cache` — transcodes in progress, resized artwork and trickplay images.
    All of it can be rebuilt.
  - `/media` — your media library. Jellyfin only reads it.

It needs Cubeship 0.7.0 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Jellyfin answers | A domain you control, pointed at your instance. |

## After installing

1. **Open the domain straight away.** Jellyfin has no default account: the
   first person to open it runs the setup wizard and creates the admin.
   Until you do, that is anyone who finds the domain.
2. In the wizard, add a library for each kind of media, pointing at a folder
   under `/media`: `/media/movies`, `/media/shows`, `/media/music`. Put the
   media there first (see below). A library can also be added later, under
   *Dashboard → Libraries*.
3. Under *Dashboard → Networking*, fill in *Known proxies* (see
   [Client addresses](#client-addresses)) and save. Jellyfin applies it after
   a restart: deploy the app.
4. In a Jellyfin app, add a server by typing `https://<your domain>`.

## Getting media in

Cubeship has no file browser, and cannot mount an object store or a network
share into an app. The media volume is a directory on the machine the app
runs on, and files get into it over SSH, from your own computer.

Find the volume's id, where `jellyfin` is the app's reference (`project/app`
when the project was named something else):

```bash
cubeship app volume list jellyfin/jellyfin
```

The id on the `/media` row names its directory on that machine:
`/var/lib/cubeship/volumes/<id>`, or under `CUBESHIP_DATA_DIR` if the
instance was installed with a different one. The directory belongs to root,
inside a data directory only root can open, so copy with root's rights:

```bash
rsync -avh --progress --rsync-path="sudo rsync" \
  ./movies/ you@your-server:/var/lib/cubeship/volumes/<id>/movies/
```

`scp` and SFTP work too when you sign in as root. Then *Scan All Libraries*
under *Dashboard → Libraries*, or wait for the scheduled scan.

A VPS disk is usually small and expensive per gigabyte, and a library is
usually large. Check the disk before copying, and leave room for `/cache`,
which grows with every transcode and trickplay image.

## What does not work here

- **Hardware transcoding.** Cubeship gives an app no devices: no GPU, no
  `/dev/dri`. Jellyfin transcodes on the CPU, which Jellyfin's own hardware
  guide calls very demanding. On a VPS, one 1080p transcode can take most of
  two cores; 4K, HEVC and HDR tone-mapping may not keep up in real time at
  all. **Aim for direct play**: keep media in formats your clients play as
  they are — H.264 video with AAC audio plays almost everywhere — and in each
  app's playback settings leave the maximum streaming quality at its highest.
  The admin dashboard lists what is playing, and whether each stream is
  direct play or a transcode.
- **Discovery on your local network.** Apps find a server by a UDP broadcast
  on port 7359, and DLNA uses UDP 1900 with host networking. Cubeship exposes
  no UDP port and gives an app no host networking. Type the server's address
  into each app instead; DLNA is not available.
- **Jellyfin's own HTTPS on 8920.** TLS ends at Cubeship's proxy, and the
  domain already serves `https://`. Leave *Enable HTTPS* off.

## Client addresses

Every request reaches Jellyfin from Cubeship's proxy. Until the proxy is a
known proxy, Jellyfin sees every visitor at the proxy's private address, so
its logs, its sessions and its remote-access rules — which tell your local
network from the internet — see everyone as local.

Add the subnet of the `cubeship` Docker network to *Known proxies*. On the
machine the app runs on:

```bash
docker network inspect cubeship -f '{{range .IPAM.Config}}{{.Subnet}} {{end}}'
```

A subnet keeps working when the proxy's container is recreated with a new
address. Jellyfin also accepts a hostname, but looks it up once, at start.

## Resetting a password

Cubeship has no console into an app. Anything that needs a shell is done over
SSH on the machine the app runs on, with `docker exec`.

*Forgot password* on the login page writes a PIN to a file in `/config`, and
does so only for a request Jellyfin counts as coming from its local network.
Once *Known proxies* is set, a request over the domain is not one. Another
admin can set a new password under *Dashboard → Users*. With a single admin,
a reset requested before *Known proxies* was set leaves its PIN here:

```bash
docker exec $(docker ps -qf name=cubeship-jellyfin-production-jellyfin) \
  sh -c 'cat /config/passwordreset*.json'
```

The container name follows the project, environment and app names you install
with.

## The volumes

The app runs as one copy on the machine its volumes are on, and a deploy
stops it for a few seconds, which interrupts anything playing.

Back up `/config` from the app's settings: every user, library, watch history
and setting is in it. `/cache` needs no backup.

**A volume's backup copies the whole directory**, with the app stopped for
the copy, to an S3 store linked from outside the instance. For `/media` that
means a copy as large as your library, and Jellyfin unavailable for as long
as it takes. Keep the originals of your media somewhere else instead, and
back up `/media` only if this server holds the only copy.

## Updating

Change `tag` in the app's settings and deploy. Back up `/config` first:
Jellyfin migrates its database on start, and cannot go back to an older
version without a restore. Before a new major version, read its release
notes — 12.0 needs a full library scan afterwards, and third-party plugins
removed first. The first start after a major upgrade can take a while; the
domain shows Jellyfin's startup page until it is done.

## Resources

The app is limited to 2 CPUs and 2 GiB of memory. That is enough for direct
play to several people and one software transcode at a time. Raise `limits`
in `template.yaml` to 4 CPUs and 4 GiB for more transcoding, large library
scans, or trickplay image generation.
