# Site and template API

Run `make site-dev` from the repository root. The site is served on port 3002;
the dashboard demo is a separate local process on port 3003. Build the image
with `make site-image`, using the repository root as Docker context.

## Ownership

| Piece | Owns |
| --- | --- |
| `core/template` | Parsing, validation, normalization and the published schema |
| `core/tools/templatecatalog` | Reading one committed templates tree, validating every directory and generating the snapshot |
| `site` | Reading and persisting the last valid snapshot, the read-only API, and the template pages |
| `core/internal/templateinstall` | Revalidating the source on an instance, installing, recording its commit and resources, and uninstalling |

Templates live in this repository's `templates/` directory. The publication
workflow validates the complete committed tree with the daemon's Go package,
then writes `snapshot.json` to the `template-catalog` branch. It never pushes
generated content onto `master`. Invalid manifests, missing Dockerfiles or
invalid icons fail the entire publication; icons are re-encoded as PNG.
Concurrent publications are serialized and pushes are never forced.

The site's Route Handler serves `/api/v1/*` and `/api/v2/*`; server-rendered pages call the same
catalog functions directly. There is no catalog process, Postgres or discovery
job. At most once per minute on demand, the site downloads the published snapshot
with a five-second timeout and a 32 MiB bound, validates its envelope and identities,
and atomically replaces its persistent copy. Concurrent readers share one refresh.
A failed fetch, invalid snapshot or failed persistence leaves the previous version
visible, including after a process restart. A first start without any valid copy
answers 503 for catalog requests; the rest of the site still works.

**Mount a persistent writable volume at `/app/site/.catalog` (owned by uid 1000)**
for the site container. `snapshot.json` holds the latest publication; SHA-named
snapshots preserve immutable icon URLs from earlier publications. They are
reconstructible cache data, but the volume is what preserves the latest good
catalog across container replacements during a GitHub outage. No rebuild or site
deployment is needed when templates change.

Local publication: `make catalog` writes `bin/catalog/snapshot.json` from
the committed tree, only after all templates pass. The workflow runs the
same generator. `templatecheck` remains available for validating working-tree
files before committing.

A template folder may declare apps, managed databases and stores, inputs, attachments, paths for volumes and published TCP ports. It may not carry instance-local domains, machine names, push-registry apps, command overrides, DNS providers, backups, certificates, firewall settings, registries or credentials. The validator in `core/template` owns those rules. The published `schema.json` is copied to the site by `make reference` and checked by `make check`.

The API under `/api/v2` keeps the list, detail, manifest, tags and icon addresses. List and detail still use `/templates/{owner}/{repo}`; entries use owner `cubeshipd` and the folder name as `repo`. The legacy `release` object in a response identifies the source commit for existing instance clients; it does not expose selectable versions. `/releases` and install-update routes are no longer offered. Tags and stars cannot be attributed per folder in a single GitHub repository, so the site and dashboard show search without those filters.

An instance reads a template's detail from the catalog and fetches `template.yaml` directly from `raw.githubusercontent.com/cubeshipd/cubeship/{commit}/templates/{folder}/template.yaml`. It validates that file again before creating anything. Installs record the commit and normalized manifest. Existing installation rows and their run history are preserved, including historical update runs. The public HTTP, CLI, MCP and dashboard no longer offer an update or release choice. An uninstall still keeps databases and stores unless the caller explicitly asks to delete their data.

Source-built apps use `repo: https://github.com/cubeshipd/cubeship` and `ref: master:templates/<slug>` in the file. The installer pins that ref to `<cataloged commit>:templates/<slug>` before creating the app. BuildKit uses the slug as the Git context directory, so `COPY` paths and Dockerfile paths remain relative to the template's folder. The catalog and CI refuse a missing Dockerfile or a mismatched ref. Renovate updates image tags in YAML and Dockerfile base images; managed database versions are limited to those the product supports.

## Compatibility and rollout

`site/catalog/v1.json` is the last validated snapshot from the old templates
repository. `/api/v1` serves it without network access, so older daemons keep
fetching manifests from the repository and commits they understand. This file
is frozen, excluded from Renovate, and must not be regenerated from the new tree.
Its source revision is recorded in its `commit` field.

The website and new daemons use `/api/v2`, whose snapshot has `version: 2` and
comes from `https://raw.githubusercontent.com/cubeshipd/cubeship/template-catalog/snapshot.json`.
The format and owner/slug identities otherwise remain the same. New daemons
try the new source path and fall back to the legacy repository only on 404.
Both source-build formats are pinned to their original repository's commit.
Existing installations, application source settings and historical runs are
not rewritten. The old repository must remain available for future redeploys.

Publish the new snapshot branch before deploying the site and daemon. Verify
both API versions, then freeze/archive the old repository; do not delete it.
Rolling back the site or daemon still works against the untouched legacy repo
and its `_catalog/snapshot.json`. The old site's persistent cache must remain
available. There is no database migration or data deletion.

Template-only PRs run catalog validation and retain the required CI aggregate;
the daemon, database and frontend jobs run when any other path changes. The
publication workflow does not deploy the site or daemon.

Public APIs keep legacy int64 icon IDs, the `release` field (a commit), tags as
an empty list and search/sort/pagination parameters. JavaScript retains IDs as
decimal strings. SHA-named cache files preserve older icon snapshots read by
the site; a cursor from a previous timestamp asks the client to restart paging.

`make check` validates Go without infrastructure. Site tests cover v1/v2,
refresh/restart/failure behavior and source URLs. Go tests cover both source
repositories, build-context pinning and complete snapshot validation.
