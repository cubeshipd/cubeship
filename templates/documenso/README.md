# Documenso on Cubeship

[Documenso](https://documenso.com) is an open-source document signing
platform: upload a PDF, place fields, send it out for signature, and get back
a digitally signed document with an audit trail.

This template installs it on a Cubeship instance with the managed Postgres it
keeps accounts, documents and audit logs in.

## What it creates

- **documenso** — the web app, from `documenso/documenso:v2.18.0`, answering
  on the domain you choose. It runs its database migrations each time it
  starts.
- **documenso-db** — a managed Postgres 17 database, attached to the app.
  Uploaded PDFs are stored in it too, so backing it up backs up everything.

## Before installing: a signing certificate

Documenso seals every completed document with a digital signature, and it
signs with a `.p12` certificate you give it. It does not make one itself:
without a certificate the app starts, and documents cannot be completed.

A self-signed certificate is enough unless your industry requires a
CA-issued one. PDF readers show its signature as valid but not trusted. Make
one on your laptop:

```bash
openssl genrsa -out private.key 2048
openssl req -new -x509 -key private.key -out certificate.crt -days 3650 \
  -subj "/O=Your Company/CN=Your Company Signing"
read -s -p "Certificate password: " CERT_PASS; echo; export CERT_PASS
openssl pkcs12 -export -out certificate.p12 -inkey private.key -in certificate.crt \
  -password env:CERT_PASS
openssl base64 -A -in certificate.p12
rm private.key certificate.crt
```

Paste the output of the last `openssl base64` line as the certificate, and the
password you typed as its password. Keep `certificate.p12` and the password:
the certificate expires after the `-days` you gave it, and replacing it means
pasting a new one into `NEXT_PRIVATE_SIGNING_LOCAL_FILE_CONTENTS` and
redeploying.

The password is not optional. Documenso cannot read the key from a `.p12`
exported without one.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Documenso answers | A domain you control, pointed at your instance. |
| The signing certificate, as base64 | The one line `openssl base64 -A` printed above. |
| The certificate's password | The password the `.p12` was exported with. |
| Your SMTP server | The host your mail provider gives you, like `smtp.mailgun.org`. |
| Its port | `587` unless your provider says otherwise. |
| The SMTP username | From your mail provider. |
| The SMTP password | From your mail provider. |
| The address Documenso sends from | An address your provider lets you send as. |
| The key session cookies are signed with | Nothing — the instance generates it. |
| The primary encryption key | Nothing — the instance generates it and shows it once. **Keep a copy.** |
| The secondary encryption key | The same. **Keep a copy.** |

Mail is not optional. A signing request is an email with a link, and so are
the completed document, reminders and the link that verifies a new account.
Without working SMTP nobody can sign anything, and the first account cannot
be verified.

Port `587` connects in plain text and upgrades with STARTTLS. A provider that
only offers TLS from the start on `465`: set the port to `465`, then change
`NEXT_PRIVATE_SMTP_SECURE` to `true` on the `documenso` app and redeploy.

## After installing

1. Open `https://<your domain>/signup` and create your account, then click the
   link in the verification email.
2. Sign up is open to anyone who finds the domain. Recipients do not need an
   account to sign, so once your team has theirs, close it: set
   `NEXT_PUBLIC_DISABLE_SIGNUP` to `true` on the `documenso` app and redeploy.
3. Check the certificate at `https://<your domain>/api/certificate-status`.
   `"isAvailable": true` means documents can be sealed. `false` means the
   pasted value or its password is wrong, or the certificate has expired.

Every account created through signup is a regular user. Documenso's admin
panel, for managing users and instance settings, needs the `ADMIN` role, set in
the database. Cubeship has no console into an app or a database, so do it over
SSH on the machine the database runs on, with the database's user from its
page in the dashboard:

```bash
docker exec cubeship-db-documenso-db psql -U <database user> -d documenso \
  -c "UPDATE \"User\" SET roles = '{USER,ADMIN}' WHERE email = 'you@example.com';"
```

The URLs assume the instance serves HTTPS. On an instance with TLS off, change
`NEXT_PUBLIC_WEBAPP_URL` to start with `http://`.

## Keys

`NEXT_PRIVATE_ENCRYPTION_KEY` and `NEXT_PRIVATE_ENCRYPTION_SECONDARY_KEY`
encrypt what Documenso stores as secrets. Changing either after installing
leaves those unreadable. Changing `NEXTAUTH_SECRET` only signs everybody out.

## Resources

The app is limited to 1 CPU and 1 GiB of memory, the minimum Documenso asks
for. Raise `limits` in `template.yaml` if sealing large documents fails.
