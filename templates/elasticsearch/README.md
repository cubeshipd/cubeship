# Elasticsearch on Cubeship

[Elasticsearch](https://www.elastic.co/elasticsearch) is a search and analytics
engine: full-text search, logs and metrics, and vector search, over a JSON
REST API.

This template installs one Elasticsearch node on a Cubeship instance, with
security on and its indices kept in a volume.

## What it creates

- **elasticsearch** — a single node, from
  `docker.elastic.co/elasticsearch/elasticsearch:9.5.3`, with its REST API on
  the domain you choose and its data in a volume at
  `/usr/share/elasticsearch/data`.

It needs Cubeship 0.7.0 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the Elasticsearch API answers | A domain you control, pointed at your instance. |
| The password for the elastic superuser | Nothing — the instance generates it and shows it once. **Keep a copy.** |

## After installing

Check it answers, with the generated password:

```bash
curl -u elastic:<password> https://<your domain>
```

Then create a user or an API key per app rather than handing out `elastic`:

```bash
curl -u elastic:<password> -X POST https://<your domain>/_security/api_key \
  -H 'Content-Type: application/json' -d '{"name": "my-app"}'
```

An app on the same instance can skip the public name and reach the node at its
internal address, shown on its page in the dashboard —
`http://cubeship-elasticsearch-production-elasticsearch:9200` with the
suggested names.

`ELASTIC_PASSWORD` only sets the password on the first start. Change it
afterwards with the `_security/user/elastic/_password` API.

## Choices this template makes

- **One node.** `discovery.type` is `single-node`: an app with a volume runs as
  one copy, so there is no cluster to form, and every index is best created
  with `number_of_replicas: 0` or it stays yellow.
- **No memory-mapped indices.** `node.store.allow_mmap` is `false`, because
  memory mapping needs `vm.max_map_count` raised on the host, which a template
  cannot do. Search is somewhat slower for it. If you raise it yourself
  (`sysctl -w vm.max_map_count=1048576`, and in `/etc/sysctl.conf`), remove
  `ES_SETTING_NODE_STORE_ALLOW__MMAP` from the app and redeploy.
- **TLS at the instance's proxy.** The domain is HTTPS; the node itself speaks
  plain HTTP, so the internal address is `http://`.
- **No health check.** Every route needs credentials, and a check answered 401
  would take the domain down. A deploy counts the node as up once its container
  stays running.

## The volume

The node stops for the length of a deploy, and takes a while to start: search
is unavailable until it has. Back the volume up from the app's settings, or use
Elasticsearch's own snapshots to an object store.

## Resources

The app is limited to 2 CPUs and 2 GiB of memory, and Elasticsearch gives half
of that to its heap. Raise `limits` in `template.yaml` for larger indices.
