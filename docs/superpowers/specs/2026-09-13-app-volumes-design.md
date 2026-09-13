# Volumes for apps

**Status:** approved design, 2026-09-13. Implementation plan to follow.

## Why

Apps on Cubeship have no persistent disk: anything a container writes is
gone on the next deploy or restart. Persistent data today is a managed
datastore or a bucket, so stateful software — RabbitMQ, Elasticsearch,
anything that keeps its state in files — cannot run as an app and cannot
be packaged as a template (`docs/design/hosted/templates.md` lists "no
volumes" as the first wall template authors hit).

A volume is a directory on one machine's disk, mounted into an app's
container at a path the app chooses, that outlives the container.

## Decisions

| Question | Decision |
| --- | --- |
| Cluster and deploy behaviour | An app with a volume runs on exactly one machine, as one copy. A deploy stops the old container before starting the new one. |
| Ownership | A volume belongs to its app. Removing it, or deleting the app, asks whether to keep the data. |
| Backups | Volumes are backed up by `internal/backup`, stopping the app for the copy. |
| Storage | A host directory under the data directory, the way datastores and managed stores keep theirs — not Docker named volumes. |

Rejected: several copies sharing one directory (corrupts RabbitMQ and
Elasticsearch), storage replicated across machines (a distributed system
to run on a one-VPS product), volumes as instance-level attachable
resources (another list, and another way to attach the wrong thing),
Docker named volumes (a second way to keep data, needing a helper
container to back up and left behind by `uninstall.sh`).

## 1. Model and runtime

### Data

Migration `00054_app_volumes.sql`:

```sql
CREATE TABLE app_volumes (
    id         BIGSERIAL PRIMARY KEY,
    app_id     BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    node_id    BIGINT NOT NULL REFERENCES nodes(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (app_id, path)
);
```

- `path` is absolute inside the container, cleaned (`path.Clean`), not
  `/`, and not under `/proc`, `/sys` or `/dev`.
- `node_id` is the machine holding the data. It is set from the app's
  single machine when the volume is created and never changes.
- The row is deleted with its app (`ON DELETE CASCADE`); whether the
  *directory* goes is the caller's choice, below.

### Where the data is

`<data dir>/volumes/<volume id>` on the machine `node_id`, keyed by id for
the reason datastores are: the id is the one thing that is not a name.

- **Control plane:** the orchestrator mounts it with
  `ContainerOpts.Binds`.
- **Worker:** `node.Placement` gains `Volumes []VolumeMount` (`id`,
  `path`); the agent binds `<its data dir>/volumes/<id>` to `path`. An
  agent from before this field ignores it, so a volume is refused on a
  machine whose reported version predates volumes.

The directory is created on first mount with mode `0o755`, owned by root;
an image that runs as another user relies on its own entrypoint to chown,
as it would under `docker run -v`.

### Placement rules

Checked in `app.Service` where placement changes, so HTTP, MCP and the
template installer share them:

- Adding a volume to an app on more than one machine, or with
  `scale > 1`, `spread` or autoscale on, is refused
  (`ErrVolumeNeedsOneCopy`).
- With a volume, setting `scale > 1`, `spread`, autoscale, or a machine
  other than `node_id` is refused (`ErrVolumePinsApp`). The error names
  the volume path and says data does not move between machines.

### Deploy

For an app with at least one volume, `Orchestrator.swap` (and the
worker's equivalent) runs **stop-then-start**:

1. Stop the running container.
2. Create and start the new one with the same binds.
3. Wait healthy. On success, remove the old container.
4. On failure, remove the new container and start the old one again;
   the deploy fails with the new container's error.

Apps without volumes keep today's start-then-stop swap. The deploy screen
and `cubeship app deploy` say that an app with a volume is unavailable
for the few seconds of the swap.

### Removing

- `DELETE /apps/{ref}/volumes/{id}` keeps the directory unless
  `delete_data=true`.
- Deleting an app takes `delete_volume_data` (default false) and applies
  it to every volume.
- A kept directory stays at `<data dir>/volumes/<id>`. It is not
  reattachable in this version; `GET /volumes/orphans` lists kept
  directories (id, path it had, app it belonged to, when) and
  `DELETE /volumes/orphans/{id}` removes one. The orphan list reads the
  directories on disk against `app_volumes`, with the app and path taken
  from a `volume.json` written beside the data when the volume is
  created.

## 2. Backups and restore

`internal/backup` gains `KindVolume` beside `KindDatastore` and
`KindInstance`, with the same schedule, retention, destinations,
`OffMachine` column and coverage report.

- **Row:** `volume_id` (`ON DELETE SET NULL`), and — stored, not joined —
  the app reference, the volume path and the node. Deleting the app or
  the volume keeps its backups.
- **Taking one:**
  1. Take the app's deploy lock so no deploy runs meanwhile.
  2. Stop the app's container.
  3. Stream `tar | gzip` of the directory through `io.Pipe` into the
     sink (object store multipart with size `-1`, or the local fallback),
     with no temporary file.
  4. Start the container again, **whether or not the copy succeeded**.
  5. Release the lock.

  Key: `cubeship/volumes/<project>-<env>-<app>/<volume id>/<timestamp>.tar.gz`,
  built by `KeyFor`. An archive with no entries beyond the root is a
  failure (`ErrEmptyArchive`): an empty directory still writes a header,
  so zero bytes is a copy that did not happen.
- **Restoring** replaces the directory and cannot be undone. Every surface
  asks for the app's name first. Steps:
  1. Take the lock and stop the container.
  2. Extract into `<data dir>/volumes/<id>.restore`.
  3. On success, rename the current directory to `<id>.previous`, rename
     `.restore` into place, and remove `.previous`. On failure, remove
     `.restore` and leave the data as it was.
  4. Start the container and release the lock.

  Refused for a backup that failed or has not finished. A backup whose
  volume was deleted can be downloaded, not restored.
- **Workers:** backing up or restoring a volume whose `node_id` is not the
  control plane is refused (`ErrVolumeOnWorker`), and the tab says volume
  backups for other machines are not available yet.
- **Roles:** the same role datastore backups take (`manageRole` in
  `internal/backup`).

The schedule form warns that the app is stopped for the duration of each
copy.

## 3. Templates, surfaces, tests

### Template file

`apps[].volumes: [{ path: string }]`, in `product/template` (decode,
schema.json, normalized manifest, docs).

Validator errors:

- `volume.path`: not absolute, `/`, or under `/proc`, `/sys`, `/dev`.
- `volume.duplicate`: two volumes of one app with the same path.
- `volume.one-copy`: a volume on an app with `scale > 1`, `spread` or
  `autoscale`.
- `volume.min-cubeship`: a template with a volume whose `minCubeship`
  does not exclude releases before volumes, so an older instance refuses
  it instead of installing without persistence.

`internal/templateinstall`:

- **Install** creates each volume after its app and before the deploy,
  recording `KindVolume` resources.
- **Update** adds volumes a release declares and never removes one it
  dropped, as for every other resource.
- **Uninstall with keep data** keeps volume directories; deleting data
  deletes them.

The catalog compiles the same validator and is redeployed with this. The
site's template page lists volumes among what a template creates.

### API, CLI, MCP

| Surface | Adds |
| --- | --- |
| HTTP | `GET/POST /apps/{ref}/volumes`, `DELETE /apps/{ref}/volumes/{id}?delete_data=`, `GET /volumes/orphans`, `DELETE /volumes/orphans/{id}`, `POST /apps/{ref}/volumes/{id}/backups`, `GET/PUT/DELETE /apps/{ref}/volumes/{id}/backups/schedule`; backups list and restore reuse `/backups` |
| CLI | `cubeship app volume list <app>`, `add <app> <path>`, `remove <app> <id> [--delete-data] --yes` |
| MCP | `list_app_volumes`, `add_app_volume`. No removal tool: deleting data stays with a person. |
| OpenAPI | Every route above, `Handle` (documented) |

Creating and removing a volume take the member role, as editing an app
does.

### Dashboard

- **App settings → Volumes tab:**
  - A table of path and machine.
  - An **Add volume** dialog asking for the path.
  - A **Remove** dialog with "Keep the data" checked by default.
  - Per-volume Backups, the same component the database Backups tab uses.
- **Placement tab:** with a volume, machine, copies and autoscale are
  disabled, with the reason beside them.
- **Deploy:** the confirm or tooltip notes the downtime.
- **Delete app dialog:** the keep-data checkbox, shown only when the app
  has volumes.
- **Backups (Platform):** volumes appear in the coverage list.
- **Preview mock:** volumes, orphans and a volume backup are seeded.

### Tests

**Unit** (`make check`):

- Placement refusals.
- Stop-then-start order and rollback, with the fake Docker.
- Validator codes.
- Tar and restore round trip in a temp dir, including a corrupt archive
  leaving data untouched.
- Placement JSON carrying volumes.
- The agent binding them.

**Postgres** (CI):

- The volumes repository, including the cascade.
- A local volume backup and restore through the service.
- A template install with a volume.
- The OpenAPI pattern list.

**Integration** (`test/integration`, CI): a container writes to a volume,
is redeployed, and the file is still there.

## Out of scope

- Showing a volume's size.
- Moving a volume between machines.
- Backups of volumes on workers.
- Reattaching a kept (orphaned) directory to an app.
- A command override in templates.
- Quotas.

## Release

Ships in the 0.7.0 line (the next release candidate). The `minCubeship`
check in the validator uses that version.
