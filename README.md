<div align="center">

# Cubeship

**A PaaS you run on your own server.** `docker push`, and it is live —
with HTTPS, a database beside it, and a second machine when one stops
being enough.

[Install](#install) · [First five minutes](#first-five-minutes) ·
[Deploy an app](#deploy-an-app) · [Databases](#databases-and-storage) ·
[More machines](#more-machines) · [Changelog](CHANGELOG.md)

</div>

---

## Contents

- [What you get](#what-you-get)
- [Install](#install)
  - [What it needs](#what-it-needs)
  - [Installing your own build](#installing-your-own-build)
- [First five minutes](#first-five-minutes)
  - [Claim the instance](#claim-the-instance)
  - [Give it a domain](#give-it-a-domain)
- [Deploy an app](#deploy-an-app)
  - [Push an image](#push-an-image)
  - [Run an image from somewhere else](#run-an-image-from-somewhere-else)
  - [Build from a Git repository](#build-from-a-git-repository)
  - [Give it a name on the internet](#give-it-a-name-on-the-internet)
- [Databases and storage](#databases-and-storage)
- [More machines](#more-machines)
- [Limits and autoscaling](#limits-and-autoscaling)
- [The CLI](#the-cli)
- [Upgrading](#upgrading)
- [Uninstalling](#uninstalling)
- [Everything else](#everything-else)

## What you get

One command on a fresh VPS, and the box is running:

|  |  |
| --- | --- |
| **A dashboard** | at your instance's address, over HTTPS |
| **A registry** | `docker push` to it and the app deploys |
| **Zero-downtime deploys** | the new container has to look healthy before the old one goes |
| **Certificates** | Let's Encrypt, renewed for you, nothing to configure |
| **Databases** | Postgres, MySQL, MariaDB, Redis, MongoDB — one click, wired into the app's environment |
| **Object storage** | a MinIO on the box, or your own S3 bucket |
| **Builds** | from a Dockerfile, or from a repository with no Dockerfile at all |
| **More machines** | add a second server and apps spread across both |
| **Charts** | what every container is using, and what the machine underneath is doing |
| **An API, a CLI and MCP** | everything the dashboard does, scriptable — and usable by an agent |

Everything Cubeship runs is a container, the daemon included: Postgres,
the registry, the proxy, the builder and every app of yours are its
siblings on one network. Nothing else is installed on the host.

## Install

On the server, as root:

```bash
curl -fsSL https://raw.githubusercontent.com/cubeshipd/cubeship/master/install.sh | sh
```

It installs Docker if the box has not got it, pulls two images, starts
the daemon, and prints the address to open and the token to claim it
with.

It is [one file](install.sh) — read it first if you would rather not
pipe a script into a shell, which is a reasonable thing to prefer.

**It installs an exact release**, not a moving tag: it asks which
release is newest and pins that number, so running the same command
again tomorrow gives you the same thing. To choose:

```bash
curl -fsSL .../install.sh | sh -s -- --version 0.1.0
```

A version with a `-rc.1` on it is a release candidate. They are
published like any other release and are never what you get by default.

### What it needs

- **Linux**, x86-64 or arm64. Debian and Ubuntu are what the installer
  knows how to put Docker on; on anything else, install Docker first and
  the script will use it.
- **Ports 80 and 443** free, for the proxy and for certificates.
- **Port 3000** free on the first install — it is where you claim the
  instance before it has a domain. The installer refuses rather than
  fighting whatever is already there.
- Root, because it installs Docker and writes to `/var/lib/cubeship`.

### Installing your own build

To run your own code instead of a published release, put the repository
on the server and build there. The build runs inside Docker, so the
server needs no Go and no Node:

```bash
git clone https://github.com/cubeshipd/cubeship && cd cubeship
sudo ./install.sh --local
```

## First five minutes

### Claim the instance

Open the address the installer printed — `http://<your-ip>:3000` — and
create the first account. It asks for the **setup token**, which the
installer printed and which is also in `/var/lib/cubeship/setup-token`.

That token is the point: without it, whoever reaches the page first is
the admin of your machine. With it, claiming the instance takes access
to the host, which is what it always meant to require. The file is
deleted the moment setup succeeds.

The first account is an admin, and it is the only account setup ever
makes. Everyone after is invited from **Account → Users**.

### Give it a domain

Point a name at the server's IP and set it in **Settings**. From then on
the instance answers there over HTTPS, and so does everything you deploy.

**You do not need one to start.** With no domain, Cubeship gives itself
an [sslip.io](https://sslip.io) address — a name that resolves to your
IP without anything being registered anywhere — so certificates and app
names work on a box you set up five minutes ago.

## Deploy an app

Apps live in an environment inside a project: `myproject/production/api`
is the whole of an app's name, and it is also its path in the registry.

Create one from the dashboard, or:

```bash
cubeship app create api --project myproject
```

An app is created **empty** and deploys anyway. It has no name on the
internet until you give it one, which is a normal thing to be: a worker
or a queue consumer has no business answering the internet.

### Push an image

The push *is* the deploy. Nothing else to press:

```bash
docker login registry.example.com          # your API key as the password
docker push registry.example.com/myproject/production/api:latest
```

`cubeship app get api` prints the exact path to push to.

### Run an image from somewhere else

Docker Hub, GHCR, DigitalOcean, ECR — anything you already publish to.
Nothing tells Cubeship when you push there, so a deploy is something you
ask for:

```bash
cubeship app create api --project myproject --source external --image nginx
cubeship app deploy api --tag 1.27
```

The image is given without a tag: which tag runs is a deploy's argument,
so an app pinned to one could never be told to run another.

This is the one that needs nothing configured at all: no domain, no
certificate, no registry. It works the minute the installer finishes.

### Build from a Git repository

Cubeship builds it on the box, from a Dockerfile you wrote or — with no
Dockerfile at all — by reading the code and working the build out.
Connect a GitHub account in **Git providers** and a push deploys it.

### Give it a name on the internet

Add a domain to the app and point a DNS record at the server. Cubeship
gets the certificate. Under an sslip.io address it already resolves, so
the name works the moment you add it.

An app can answer at several names, and **each name carries its own
port** — one image exposing an API and an admin panel is two names.

## Databases and storage

A database belongs to the **instance**, not to a project: on one box the
usual shape is a single Postgres serving several small apps, and those
apps are routinely in different projects.

```bash
cubeship db create pg --engine postgres
cubeship db attach pg --app myproject/production/api
```

The app receives `DATABASE_URL` and its parts in its own environment
from its next deploy — nothing to copy, and no credential passing
through your hands. A second database on one app takes a prefix.

Object storage works the same way: a MinIO Cubeship runs, or an S3
bucket you already have. An attached app gets `S3_ENDPOINT`,
`S3_BUCKET`, the keys and the rest.

**There are no backups.** Deleting a database deletes its data, and
nothing here copies it anywhere. That is worth knowing before you put
something you cannot lose on it.

## More machines

Add a server, and the instance becomes a cluster:

```bash
cubeship server add eu-1
```

It prints the command to run on the new box, address and credential
already in it. The new machine **dials home** and nothing dials it: no
port to open, no certificate, no firewall hole — a box behind NAT joins
with one outbound connection.

The machines share a private network, encrypted, and container names
mean the same thing on every one of them.

**Every name still arrives at your instance**, whichever machine runs
the app. One DNS record, one certificate store, and moving an app
between machines touches neither.

```bash
cubeship app place api --on eu-1               # move it
cubeship app place api --replicas 4            # four copies, spread
cubeship app place api --everywhere            # and on every machine that joins
```

Scaling takes effect at once, in both directions.

## Limits and autoscaling

Cap what one copy of an app — or a database, or a store — may take:

```bash
cubeship app limits api --cpu 1 --memory 512Mi
```

**Changing a limit restarts nothing.** It is the one part of a running
container Docker can change, so raising an app's memory is a request
rather than a redeploy.

Or hand the replica count over entirely:

```bash
cubeship app autoscale api --min 2 --max 8 --cpu 70
```

It works from the average CPU across the app's copies — the number on
the app's own chart, where 100 is one core. A maximum is required:
without one, a loop of requests is a loop of replicas.

## The CLI

`cubeship` talks to the instance's API from your machine.

```bash
cubeship login https://cubeship.example.com <your-api-key>
cubeship app list
cubeship app logs api
cubeship app env set api DATABASE_POOL=10
```

Make an API key under **Account** in the dashboard. Every noun has
`--help`, and every command it can run is something the dashboard can
too — they are the same API.

Building the CLI for your own machine, on macOS or Linux:

```bash
git clone https://github.com/cubeshipd/cubeship && cd cubeship
make build          # bin/cubeship
```

## Upgrading

Run the installer again. It pulls the newest release and replaces the
containers; nothing under the data directory is touched.

```bash
curl -fsSL https://raw.githubusercontent.com/cubeshipd/cubeship/master/install.sh | sh
```

The dashboard shows you what changed the next time you open it. What is
in each release is in the [changelog](CHANGELOG.md).

## Uninstalling

```bash
sudo ./uninstall.sh            # the containers; your data is kept
sudo ./uninstall.sh --purge    # and the data, permanently
```

The default is deliberately not the destructive one: removing the
software is not the same as asking to lose your database. Installing
again brings the same instance back.

## Everything else

- **API reference** — `https://your-instance/docs`, and the OpenAPI
  document at `/openapi.json`.
- **MCP** — `https://your-instance/mcp`, authenticated with the same API
  key. An agent can create projects, deploy apps and wire up databases;
  it cannot read a secret or change a container's limits.
- **Working on Cubeship itself** — [CONTRIBUTING.md](CONTRIBUTING.md).
