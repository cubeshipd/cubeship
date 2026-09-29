# Templates

`cubeshipd/cubeship-templates` is the only source of public templates. One directory is one template, named with just the slug (for example, `umami`), with `template.yaml`, `README.md` and `icon.png`. The directory is its stable API identity; top-level `name` in the YAML is its display name. `version: 1` identifies the YAML format, not a release. Changes to an installed app are made on its instance; template updates are not offered.

## Ownership

| Piece | Owns |
| --- | --- |
| `product/template` | Parsing, validation, normalization and the published schema |
| `hosted/catalog` | Reading one commit of the templates repository, validating every directory, keeping the latest valid snapshot in Postgres, and serving the read-only API |
| `hosted/site` | Pages over the catalog API |
| `product/internal/templateinstall` | Revalidating the source file on an instance, installing it, recording the source commit and resources, and uninstalling it |

The catalog asks GitHub for the `main` commit and downloads its archive at that SHA. It validates every folder before writing anything. Missing files, an invalid manifest or icon, or a download failure leave the previous snapshot visible. The Postgres `Publish` transaction writes the new entries, moves the listing pointers and hides directories removed from the repository together. Existing release rows remain for historical installation records; the public listing only points to the current commit. The database is retained so a service restart during a GitHub outage can still serve the previous snapshot. A token is optional for this public repository.

A template folder may declare apps, managed databases and stores, inputs, attachments, paths for volumes and published TCP ports. It may not carry instance-local domains, machine names, push-registry apps, command overrides, DNS providers, backups, certificates, firewall settings, registries or credentials. The validator in `product/template` owns those rules. The published `schema.json` is copied to the site by `make reference` and checked by `make check`.

The API under `/api/v1` keeps the list, detail, manifest, tags and icon addresses. List and detail still use `/templates/{owner}/{repo}`; new rows use owner `cubeshipd` and the folder name as `repo`. The legacy `release` object in a response identifies the source commit for existing instance clients; it does not expose selectable versions. `/releases` and install-update routes are no longer offered. Tags and stars cannot be attributed per folder in a single GitHub repository, so the site and dashboard show search without those filters.

An instance reads a template's detail from the catalog and fetches `template.yaml` directly from `raw.githubusercontent.com/cubeshipd/cubeship-templates/{commit}/{folder}/template.yaml`. It validates that file again before creating anything. Installs record the commit and normalized manifest. Existing installation rows and their run history are preserved, including historical update runs. The public HTTP, CLI, MCP and dashboard no longer offer an update or release choice. An uninstall still keeps databases and stores unless the caller explicitly asks to delete their data.

The rollout order is: publish the templates repository; publish the daemon and site that understand its paths; then switch the catalog service to the monorepo reader. The catalog keeps its last valid snapshot if the new repository is unavailable. A daemon older than this migration reads template source from individual repositories, so it must be updated before it can install the new catalog entries. Installed resources do not depend on those repositories remaining available.

`make check` validates the product and hosted Go modules without infrastructure. The templates repository runs `go run ./tools/templatecheck ../..` from a checkout of `cubeship/product` in CI, checking all folders with the same validator the catalog uses.
