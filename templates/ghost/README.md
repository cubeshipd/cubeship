# Ghost on Cubeship

[Ghost](https://ghost.org) is an open-source publishing platform: a website,
a blog and a newsletter in one, with paid memberships built in.

This template installs it on a Cubeship instance with the managed MySQL it
needs, and a volume for everything it keeps in files.

## What it creates

- **ghost** — Ghost, from `ghost:6.63.0`, answering on the domain you choose,
  with a volume at `/var/lib/ghost/content`: uploaded images and files,
  themes, and the routes and redirects files.
- **ghost-db** — a managed MySQL 8.0 database, attached to the app. Posts,
  members, staff and settings are in it.

It needs Cubeship 0.7.0 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the site answers | A domain you control, pointed at your instance. |
| Your SMTP server | The host your mail provider gives you, like `smtp.mailgun.org`. |
| Its TLS port | `465` unless your provider says otherwise. |
| The SMTP username | From your mail provider. |
| The SMTP password | From your mail provider. |
| The address Ghost sends from | An address your provider lets you send as. |

Mail is not optional. Ghost emails a code whenever a staff member signs in
from a device it has not seen, and the same mail carries staff invites and
password resets. Without it, the first sign-in works and the next one on a
new browser does not.

Ghost connects to port `465` with TLS from the start. A provider that only
offers STARTTLS on `587`: set the port to `587`, then change
`mail__options__secure` to `false` on the `ghost` app and redeploy.

This is transactional mail only. Newsletters go out through Mailgun,
configured separately in Ghost's settings.

## After installing

1. **Open `https://<your domain>/ghost` straight away.** Ghost has no default
   account: the first person to open it creates the owner. Until you do, that
   is anyone who finds the domain.
2. Before signing out, sign in again from a private window. Ghost emails a
   code to the owner's address; if it never arrives, fix the SMTP settings
   while you still have a session.

The SMTP settings are variables on the `ghost` app. Change them and redeploy.

## What does not work here

- **Social web (ActivityPub).** Ghost expects `/.ghost/activitypub/` on its
  domain to reach a separate service, and a domain on Cubeship sends every
  path to the one app. It is on by default: turn *Social web* off in Ghost's
  settings so Ghost stops trying.
- **Web analytics.** Ghost's built-in traffic analytics need a Tinybird
  account and a tracking service beside Ghost. Without them Ghost leaves
  them off.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Back up both the volume, for images and themes, and the
database, for everything else: one without the other is a site with broken
images or no posts.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Raise `limits` in
`template.yaml` for a busy site.
