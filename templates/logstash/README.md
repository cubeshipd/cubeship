# Logstash on Cubeship

[Logstash](https://www.elastic.co/logstash) is Elastic's data processing
pipeline: it takes in logs and events, parses and enriches them, and ships
them to Elasticsearch.

This template installs one Logstash on a Cubeship instance, taking events over
HTTP on a domain and over Beats on a TCP port, and writing them to Elasticsearch.
It pairs with the
[Elasticsearch template](https://github.com/cubeshipd/cubeship/tree/master/templates/elasticsearch).

## What it creates

- **logstash** — Logstash `9.5.3`, built on the instance from the `Dockerfile`
  in this repository. Its HTTP input answers on the domain you choose; its
  queue is kept in a volume at `/usr/share/logstash/data`.

It needs Cubeship 0.7.2 or newer, **an admin to install it** — the app is
built on the instance, and only admins build — and an Elasticsearch to write
to.

## Why it is built

The published image, `docker.elastic.co/logstash/logstash`, runs an example
pipeline that listens for Beats and prints every event to its log. A pipeline
is a file, and Cubeship cannot mount a file into a container, so the
`Dockerfile` here is that image with [`logstash.conf`](logstash.conf) in the
example's place. To change what Logstash does, edit that file — see
[Changing the pipeline](#changing-the-pipeline).

## What you are asked

| Input | What to give |
| --- | --- |
| Where the HTTP input answers | A domain you control, pointed at your instance. |
| The username senders give over HTTP | Anything; `logstash` unless you have a reason. |
| The password senders give over HTTP | Nothing — the instance generates it and shows it once. **Keep a copy.** |
| Where Elasticsearch answers | Its internal address. The default is the Elasticsearch template's, installed with its suggested names; the address is on the Elasticsearch app's page in the dashboard. |
| The Elasticsearch user Logstash writes as | `elastic`, or better the user from [A user for Logstash](#a-user-for-logstash). |
| That user's password | Its password. |
| The port Beats answers on | `5044` by default. Choose a port from `1024` to `65535`, and open it in your provider's firewall too. |

## Sending events

**From outside the instance, over HTTP.** POST to the domain, with the
credentials you were given:

```bash
curl -u logstash:<password> https://<your domain> \
  -H 'Content-Type: application/json' \
  -d '{"message": "hello", "service": "billing"}'
```

A JSON object becomes one event, and a JSON array one event per element. Any
other content type becomes one event whose `message` is the body. Logstash
answers `200 ok` once the event is in its queue. A request without the
credentials is answered `401`, whatever its method or path.

**Over Beats.** Point Filebeat's or another Beat's Logstash output at the
instance address and the port you chose:

```yaml
output.logstash:
  hosts: ["<your VPS address>:<chosen port>"]
```

From an app on the same instance, the internal name follows the project,
environment and app names you install with:

```yaml
output.logstash:
  hosts: ["cubeship-logstash-production-logstash:5044"]
```

Beats has no authentication here, so protect the published port with your
provider's firewall or a network restriction. The HTTP input
answers inside the instance too, at
`http://cubeship-logstash-production-logstash:8080`; Beats remains available
there on port `5044`.

## Where events land

In the data stream `logs-generic-default`, which Elasticsearch creates on the
first event with its built-in `logs` index template and lifecycle. Look at
them with:

```bash
curl -u elastic:<password> 'https://<elasticsearch domain>/logs-generic-default/_search?size=5'
```

An event carrying its own `data_stream.type`, `data_stream.dataset` or
`data_stream.namespace` field goes to the stream those name, like
`logs-billing-production`. To send everything from this pipeline to one
dataset, set `data_stream_dataset` in the output in `logstash.conf`.

## A user for Logstash

`elastic` can do anything, including delete every index. Logstash needs only
to create logs data streams and write to them. In Elasticsearch, create a role
for that and a user with it:

```bash
curl -u elastic:<password> -X POST https://<elasticsearch domain>/_security/role/logstash_writer \
  -H 'Content-Type: application/json' -d '{
    "cluster": ["monitor"],
    "indices": [{ "names": ["logs-*-*"], "privileges": ["create_doc", "auto_configure"] }]
  }'

curl -u elastic:<password> -X POST https://<elasticsearch domain>/_security/user/logstash_internal \
  -H 'Content-Type: application/json' -d '{
    "password": "<a long random password>",
    "roles": ["logstash_writer"]
  }'
```

Give `logstash_internal` and its password at install, or change
`ELASTICSEARCH_USER` and `ELASTICSEARCH_PASSWORD` on the app afterwards and
redeploy. If you change the output to write somewhere other than `logs-*-*`,
add that pattern to the role.

An API key is not offered: the Elasticsearch output sends one only over TLS,
and the internal address is plain HTTP.

## Choices this template makes

- **A persistent queue.** `queue.type` is `persisted`, in the volume. An event
  Logstash has answered `200` for is on disk until Elasticsearch has it, so a
  deploy, a restart or Elasticsearch being down loses nothing. The queue holds
  up to 1 GiB; once full, Logstash stops taking events in — the HTTP input
  answers `429` and Beats waits — until Elasticsearch catches up.
- **A dead letter queue.** An event Elasticsearch refuses — a field whose type
  conflicts with the mapping, say — is written to
  `/usr/share/logstash/data/dead_letter_queue` rather than dropped, and removed
  after 7 days. Read it back with the
  [`dead_letter_queue` input](https://www.elastic.co/docs/reference/logstash/plugins/plugins-inputs-dead_letter_queue)
  in a pipeline of your own.
- **No health check.** The HTTP input answers `401` to a request without
  credentials and turns one with them into an event, and the check probes the
  domain's port. A deploy counts Logstash as up once its container stays
  running. Its monitoring API answers on port `9600` inside the instance:
  `curl http://cubeship-logstash-production-logstash:9600/` from another app.
- **TLS at the instance's proxy.** The domain is HTTPS; the HTTP input itself
  speaks plain HTTP.

## Changing the pipeline

Filters go in `logstash.conf` between `input` and `output` — `grok` to parse
lines, `date` to take the timestamp from the event, `mutate` to rename fields.
Any `${NAME}` in the file is read from the app's environment, so a value that
differs per install belongs in an environment variable, not in the file.

The app builds from this repository at `ref`. Fork it, change the pipeline,
and change the app's repository and ref to your fork's.

Logstash settings — `pipeline.workers`, `queue.max_bytes`, `log.level` — are
environment variables on the app: upper case, with underscores for dots, like
`PIPELINE_WORKERS`.

## The volume

Logstash stops for the length of a deploy, and takes a while to start: while
it is down, HTTP senders get an error and Beats retries. What was in the queue
is delivered once it is back. Back the volume up from the app's settings if
the queue matters to you; Elasticsearch holds everything already delivered.

## Updating

Change the tag in the `Dockerfile` — keeping it the same version as your
Elasticsearch is simplest — release this repository, and point `ref` at the
new release.

## Resources

The app is limited to 1 CPU and 2 GiB of memory, and Logstash's heap is 1 GiB.
For heavier pipelines, raise `limits` in `template.yaml` and set `LS_JAVA_OPTS`
to `-Xms2g -Xmx2g` or so, keeping the heap to about half the memory limit.
