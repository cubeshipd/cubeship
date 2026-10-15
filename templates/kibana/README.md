# Kibana on Cubeship

[Kibana](https://www.elastic.co/kibana) is the interface to Elasticsearch:
search and explore your indices, build dashboards, and manage the cluster from
a browser.

This template installs Kibana on a Cubeship instance and connects it to an
Elasticsearch you already run — made for the
[Elasticsearch template](https://github.com/cubeshipd/cubeship/tree/master/templates/elasticsearch).

## What it creates

- **kibana** — Kibana, from `docker.elastic.co/kibana/kibana:9.5.3`, answering
  on the domain you choose.

Nothing else: Kibana keeps its saved objects, users' sessions and settings in
Elasticsearch, so it needs no database and no volume.

## Before installing

Kibana must run the **same version as Elasticsearch**: 9.5.3, which is what the
Elasticsearch template installs.

Kibana will not sign in to Elasticsearch as `elastic`. It needs a token for the
`elastic/kibana` service account, and the token has to be created on the
Elasticsearch you already have. With the `elastic` password from the
Elasticsearch install:

```bash
curl -u elastic:<password> -X POST \
  https://<your Elasticsearch domain>/_security/service/elastic/kibana/credential/token/cubeship
```

It answers:

```json
{"created":true,"token":{"name":"cubeship","value":"AAEAAWVsYXN0aWMva2liYW5h..."}}
```

The `value` is the token. Elasticsearch shows it only this once.

A token name can be used once. To make another, pick a new name, or delete the
old one first with the same URL and `-X DELETE`.

## What you are asked

| Input | What to give |
| --- | --- |
| Where Kibana answers | A domain you control, pointed at your instance. |
| Your Elasticsearch's address | Keep the default if you installed the Elasticsearch template with the names it suggests. Otherwise, the internal address on the Elasticsearch app's page. |
| A service account token for Kibana | The `value` from the command above. |
| The key that encrypts saved objects | Nothing — the instance generates it. |
| The key that encrypts sessions | Nothing — the instance generates it. |
| The key that encrypts reports | Nothing — the instance generates it. |

## After installing

Open the domain and sign in as `elastic` with the Elasticsearch password. Then
create a user for each person under *Stack Management → Users*, rather than
sharing `elastic`.

## Choices this template makes

- **A service account token, not the `kibana_system` password.** Both work;
  the token is one request against Elasticsearch, where the password would
  need resetting first, and a token can be revoked on its own.
- **Fixed encryption keys.** Without them Kibana makes new ones on every
  start: everyone is signed out, and encrypted saved objects — connector
  secrets, alerting rules' API keys — can no longer be read. Do not change
  them after installing.
- **No volume.** Kibana keeps its server UUID in `/usr/share/kibana/data`, so a
  deploy gives it a new one, and stack monitoring counts it as a new Kibana.
  Everything a person creates is in Elasticsearch.
- **TLS at the instance's proxy.** Kibana speaks plain HTTP to the proxy, and
  `server.publicBaseUrl` is the HTTPS domain. Its cookies are marked
  secure, so the domain has to stay on HTTPS.
- **A health check that needs no sign-in.** `/api/status` answers an anonymous
  request with only the overall level: 200 while Kibana can serve, 503 while it
  starts or cannot reach Elasticsearch. The domain answers once Kibana is
  ready.

## Resources

The app is limited to 1 CPU and 2 GiB of memory, which Elastic recommends as a
minimum for alerting, reporting and SLOs. Kibana sizes its heap from that
limit. Raise `limits` in `template.yaml` for heavy reporting.
