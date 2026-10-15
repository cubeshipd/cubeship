# Cubeship templates

Each directory is a template: `template.yaml`, `README.md` and a square PNG
icon. The directory is its stable slug; `name` in the manifest is its display
name. `version: 1` describes the YAML format, not a template release.

Add or change templates here. Renovate updates their image tags and Dockerfile
base images. Installed applications keep their existing configuration; changes
here apply to new installations.

Source-built apps use `repo: https://github.com/cubeshipd/cubeship` and
`ref: master:templates/<slug>`. Keep the Dockerfile and everything it copies
inside that directory. The daemon pins the ref to the cataloged commit before
building, with `templates/<slug>` as the context directory.

Validate working files from the repository root:

```sh
go -C core run ./tools/templatecheck ../templates
```

`make catalog` generates `bin/catalog/snapshot.json` from the committed tree.
CI validates every template together. On relevant pushes to `master`, the
publication workflow writes the snapshot to the `template-catalog` branch.
The site reads that publication at most once per minute; no site deployment
is required for template changes.

The public API is `/api/v2`. `/api/v1` keeps a frozen catalog for older daemons,
whose manifest URLs and source commits remain in `cubeshipd/cubeship-templates`.
Do not delete that repository or rewrite its history.
