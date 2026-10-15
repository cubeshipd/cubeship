# GitLab on Cubeship

[GitLab](https://about.gitlab.com) Community Edition is a self-hosted DevOps
platform: Git repositories, merge requests, issues, wikis, a package registry
and CI/CD pipelines, in one Linux package.

This template installs it on a Cubeship instance, with a managed Redis and
volumes for the repositories, the database and its configuration. The Postgres
is the one inside the image, and it has to be — see [Why the database is not a
managed one](#why-the-database-is-not-a-managed-one).

**GitLab is the heaviest thing you can put on an instance.** Upstream's baseline
for a single machine is 8 vCPU and 16 GB of memory; this template ships
upstream's [memory-constrained
settings](https://docs.gitlab.com/omnibus/settings/memory_constrained_envs/)
instead, and asks the machine for 4 CPU and 4 GiB. Give it a VPS with at least
8 GB of memory and 40 GB of disk free, and expect a handful of developers, not
a hundred.

## What it creates

- **gitlab** — GitLab CE, from `gitlab/gitlab-ce:19.3.2-ce.0`, answering on the
  domain you choose, with three volumes:
  - `/etc/gitlab` — `gitlab.rb` and `gitlab-secrets.json`, the keys every
    encrypted column in the database is readable with.
  - `/var/opt/gitlab` — every repository, LFS object, upload, artifact and
    package, **and the Postgres database**.
  - `/var/log/gitlab` — the logs, kept because they are where a failed start
    explains itself. `logrotate` inside the image bounds them.
- **gitlab-redis** — a managed Redis 7.4, holding sessions and the background
  queues. The Redis inside the image is turned off.

It needs Cubeship 0.7.2 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where GitLab answers | A domain you control, pointed at your instance. It becomes `external_url`, which GitLab writes into every clone URL, webhook and email. |
| The password for the root account | Nothing — the instance generates it and shows it once. **Keep a copy**; it is read only on the first start, while GitLab creates `root`. |
| The port Git over SSH answers on | `2222` by default, from `1024` to `65535`. Not `22`, which is the server's own SSH. |

## Why the database is not a managed one

GitLab loads its schema with `psql --single-transaction`, which takes a lock per
partitioned table and needs `max_locks_per_transaction` far above Postgres's
default of `64`. Omnibus sets `128` for its own. A managed Postgres on Cubeship
runs the official image's defaults and takes no server parameters, so the very
first migration ends in:

```
ERROR:  out of shared memory
HINT:  You might need to increase "max_locks_per_transaction".
```

and no reconfigure ever finishes. Nothing in the template can raise it, so the
bundled Postgres runs instead, with its data in the `/var/opt/gitlab` volume.
That is also why `gitlab-backup` below, and not a Cubeship database dump.

## The first start takes about ten minutes

GitLab reconfigures itself and runs its database migrations before anything
answers, and on a small machine that is five to fifteen minutes. The domain
returns *502* or *503* the whole time, and the deploy itself is reported as
finished long before. Watch it get there:

```bash
cubeship app logs gitlab/production/gitlab --follow
```

It is up when the sign-in page answers `200`:

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://<your domain>/users/sign_in
```

`/-/health` is no use for this from outside: GitLab answers it only to
localhost and returns `404` to everyone else, even once it is running.

Then sign in as `root` with the password above.

## Close sign-ups

Anyone who finds the domain can register an account until you stop them. Do it
first: **Admin → Settings → General → Sign-up restrictions**, clear *Sign-up
enabled*, and save. Add people afterwards under **Admin → Users**.

If you lose the root password, reset it from the machine the app runs on —
Cubeship has no console into an app:

```bash
docker exec -it $(docker ps -qf name=cubeship-gitlab-production-gitlab) \
  gitlab-rake "gitlab:password:reset[root]"
```

## Git over SSH and HTTPS

Both work. Over SSH, add your key under **Settings → SSH keys** and clone
with the URL GitLab shows, which carries the port:

```bash
git clone ssh://git@<your domain>:2222/<group>/<project>.git
```

SSH is published on the port you answered — `2222` unless you chose
another — because `22` is the server's own SSH. If your provider has a
firewall in front of the machine, open that port there as well; Cubeship
opens it in the server's own firewall. Nothing proxies it: it is GitLab's
sshd, directly.

Over HTTPS, clone `https://<your domain>/<group>/<project>.git` and push
with your password or a personal access token from
**Settings → Access tokens** — required once two-factor sign-in is on. Git
LFS works over the same URL.

## What is turned off, and why

| Off | Why |
| --- | --- |
| The container registry | It needs a second domain and a port of its own; an app on Cubeship has one. Use the instance's own registry, or push elsewhere. |
| Prometheus, Alertmanager, the exporters, KAS | Around 300 MB of memory for monitoring this template does not read. Cubeship charts the container's CPU and memory itself. |
| Puma's cluster mode | `worker_processes = 0` runs one Puma process. It is the single largest saving upstream lists, and the reason 4 GiB is enough. |
| Let's Encrypt | Traefik holds the certificate; nginx inside the image serves plain HTTP on port 80. |

## Runners

No runner is included, so pipelines stay queued. `gitlab-runner` starts a
container per job and needs the Docker socket, which Cubeship does not give an
app. Run it on another machine with Docker and register it against
`https://<your domain>` with a token from **Admin → CI/CD → Runners** — see
upstream's [runner install
guide](https://docs.gitlab.com/runner/install/). To hide CI instead, clear
*CI/CD* under **Admin → Settings → General → Visibility and access controls**.

## Mail

No SMTP is set up, and GitLab runs without it — but then nobody is notified of
a merge request and nobody can reset a password by email. To add it, edit
`GITLAB_OMNIBUS_CONFIG` on the `gitlab` app, appending:

```ruby
gitlab_rails['smtp_enable'] = true;
gitlab_rails['smtp_address'] = 'smtp.example.com';
gitlab_rails['smtp_port'] = 587;
gitlab_rails['smtp_user_name'] = 'apikey';
gitlab_rails['smtp_password'] = 'the password';
gitlab_rails['smtp_domain'] = 'example.com';
gitlab_rails['smtp_authentication'] = 'login';
gitlab_rails['smtp_enable_starttls_auto'] = true;
gitlab_rails['gitlab_email_from'] = 'gitlab@example.com';
```

Then redeploy. Every statement is on one line, separated by `;` — the variable
is one long Ruby string, and the whole of `gitlab.rb` can go in it.

## Settings live in that one variable

`GITLAB_OMNIBUS_CONFIG` is written into `/etc/gitlab/gitlab.rb` on every start,
so a value there beats anything edited in the file by hand. Upstream's
[configuration
reference](https://docs.gitlab.com/omnibus/settings/configuration/) is the list
of what may go in it. Changing the domain means changing `external_url` here
too — GitLab does not learn it from the request.

## Resources

The app is limited to 4 CPU and 4 GiB — everything but Redis is inside it. If
the machine has more to spare, give Puma its workers back and GitLab gets several
requests at a time: set `puma['worker_processes'] = 2` and raise the app's
memory to 6 GiB or more. If the container is killed and restarted under load,
that is the memory limit, not a crash.

`shared_buffers` and `effective_cache_size` are spelled out in
`GITLAB_OMNIBUS_CONFIG` because omnibus sizes Postgres from what it reads as the
machine's memory, and inside a container that is the *host's*. A
32 GB host would otherwise hand a 4 GiB container 8 GB of shared buffers, and
Postgres would not start.

## Backups

Cubeship cannot dump this database: it is inside the app. Use GitLab's own
backup, from the machine the app runs on:

```bash
docker exec -t $(docker ps -qf name=cubeship-gitlab-production-gitlab) \
  gitlab-backup create
```

It writes a tar into `/var/opt/gitlab/backups`. **It does not include
`/etc/gitlab`**, and a backup restored without `gitlab-secrets.json` leaves
every encrypted value — two-factor secrets, CI variables, tokens — unreadable.
Copy that file too, and keep both off the machine.

## Upgrading

Change `tag` and redeploy, but **GitLab has required stops**: skipping one
leaves migrations that cannot run. Check upstream's [upgrade
path](https://docs.gitlab.com/update/upgrade_paths/) for the versions between
the one installed and the one wanted, and take them in order. The Postgres in
the volume is upgraded by the image itself, which is one fewer thing to line up
than an external one would be.
