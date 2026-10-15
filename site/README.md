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

The templates repository publishes `_catalog/snapshot.json` from its `main`
workflow after validating all directories with the same Go package the daemon
uses. The generator reads `git archive` at one SHA, so filenames, manifests and
source references cannot mix revisions. Invalid manifests, missing Dockerfiles
or invalid icons fail the whole publication. Icons are re-encoded as PNG.
Generated content is committed by GitHub Actions, with publication-only commits
excluded from the trigger. A rejected push never overwrites a newer tree;
rerun the workflow on current main if another push won the race.

The site's Route Handler serves `/api/v1/*`; server-rendered pages call the same
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

Local publication: `make catalog TEMPLATES_DIR=../cubeship-templates` writes the
snapshot in that checkout, only after all templates pass. The workflow runs the
same generator. `templatecheck` remains available for validating working-tree
files before committing.

A template folder may declare apps, managed databases and stores, inputs, attachments, paths for volumes and published TCP ports. It may not carry instance-local domains, machine names, push-registry apps, command overrides, DNS providers, backups, certificates, firewall settings, registries or credentials. The validator in `core/template` owns those rules. The published `schema.json` is copied to the site by `make reference` and checked by `make check`.

The API under `/api/v1` keeps the list, detail, manifest, tags and icon addresses. List and detail still use `/templates/{owner}/{repo}`; new rows use owner `cubeshipd` and the folder name as `repo`. The legacy `release` object in a response identifies the source commit for existing instance clients; it does not expose selectable versions. `/releases` and install-update routes are no longer offered. Tags and stars cannot be attributed per folder in a single GitHub repository, so the site and dashboard show search without those filters.

An instance reads a template's detail from the catalog and fetches `template.yaml` directly from `raw.githubusercontent.com/cubeshipd/cubeship-templates/{commit}/{folder}/template.yaml`. It validates that file again before creating anything. Installs record the commit and normalized manifest. Existing installation rows and their run history are preserved, including historical update runs. The public HTTP, CLI, MCP and dashboard no longer offer an update or release choice. An uninstall still keeps databases and stores unless the caller explicitly asks to delete their data.

Source-built apps use `repo: https://github.com/cubeshipd/cubeship-templates` and `ref: main:<slug>` in the file. The installer pins that ref to `<cataloged commit>:<slug>` before creating the app. BuildKit uses the slug as the Git context directory, so `COPY` paths and Dockerfile paths remain relative to the template's folder. The catalog and CI refuse a missing Dockerfile or a mismatched ref. Renovate updates image tags in YAML and Dockerfile base images; managed database versions are limited to those the product supports.

## Migration from the catalog service

Land the Cubeship changes first so the templates workflow can find `core/` and
the generator. Then publish the templates workflow and its first snapshot.
Deploy the site with the persistent cache volume and verify `/api/v1/templates`,
detail, manifest and icon responses before retiring the old catalog service.
Keep the old database backup; this code does not delete production services or data.
Existing instance installation records and historical update runs stay untouched.
Recovery can still roll back an update interrupted on an older daemon; new template
updates and their unused planning/execution code are removed.

Public v1 keeps owner/name paths, the `release` response field (now a commit),
legacy int64 icon IDs, tags as an empty list, and accepted search/sort/pagination
parameters. IDs stay decimal strings internally in JavaScript to avoid rounding.
A cursor for a previous snapshot's timestamp asks the caller to restart pagination.
Historical icon snapshots are retained from the time this site first reads them;
icons that only existed in the retired database require that database's backup.

`make check` validates the Go module without infrastructure. Site tests cover v1
contracts and snapshot refresh/restart/failure behavior. The templates workflow
checks every committed folder before publishing.
