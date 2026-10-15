# Cal.com on Cubeship

[Cal.com](https://cal.com) is open-source scheduling: share a link, and
people book time on your calendar without the back-and-forth.

This template installs it on a Cubeship instance with the managed Postgres it
keeps users, event types and bookings in.

## What it creates

- **cal-com** — the Cal.com web app, from `calcom/cal.com:v6.2.0`, answering
  on the domain you choose.
- **cal-com-db** — a managed Postgres 18 database, attached to the app.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Cal.com answers | A domain you control, pointed at your instance. |
| The secret sessions are signed with | Nothing — the instance generates it and shows it once. |
| The key Cal.com encrypts stored credentials with | Nothing — the instance generates it and shows it once. **Keep a copy**: a new key cannot read the calendar and app credentials saved under the old one. |
| Your SMTP server | The host your mail provider gives you, like `smtp.mailgun.org`. |
| Its port | `587` unless your provider says otherwise. |
| The SMTP username | From your mail provider. |
| The SMTP password | From your mail provider. |
| The address Cal.com sends from | An address your provider lets you send as. |

Mail is not optional. A booking's confirmation, its cancellation, the
organizer's notice that somebody booked, and password resets are all emails.
Without them Cal.com still takes bookings, and nobody is told.

Port `465` gets TLS from the first byte; any other port starts plain and
upgrades with STARTTLS.

## After installing

1. **The first start takes a few minutes.** Cal.com migrates its database and
   fills its app store before it listens, and the domain answers only once it
   does.
2. **Open `https://<your domain>/auth/setup` straight away.** Cal.com has no
   default account: the first person to open it creates the admin. Until you
   do, that is anyone who finds the domain.
3. Book a test event with an address you read, to see that mail arrives.

The SMTP settings are variables on the `cal-com` app. Change them and
redeploy.

The URLs assume the instance serves HTTPS. On an instance with TLS off, change
`NEXT_PUBLIC_WEBAPP_URL` and `NEXTAUTH_URL` to start with `http://`.

## Calendars and video

Google Calendar, Outlook, Zoom and the other integrations each need an OAuth
app you register with that provider, pointed back at your domain. None is set
up here; follow Cal.com's self-hosting docs for the one you want, add what it
asks for to the `cal-com` app, and redeploy.

## What is not included

- **API v2.** Cal.com's newer API is a separate service with its own image,
  and this template does not run it. The web app works without it.
- **Enterprise features.** Cal.com is AGPLv3, except the code under its `ee`
  directories, which is under a commercial license and needs a license key
  from Cal.com.

## Resources

The app is limited to 1 CPU and 2 GiB of memory. Raise `limits` in
`template.yaml` for a busy instance.
