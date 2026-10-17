---
name: core-template
description: Change Cubeship template parsing, normalization, catalog publication, or installation while keeping their shared contract aligned.
---

# Core templates

`core/internal/template` is the single validator and normalizer used by catalog tools and instance installation. Keep rules there and avoid a second implementation in `tools/`, `templateinstall/`, or the site.

- Template rules and schema must agree; `TestJSONSchemaMatchesTheDecoder` checks the embedded schema. `make reference` copies it to `site/public/schema/template/v1.json` when it changes.
- `tools/internal/catalog` validates a committed templates tree and publishes a snapshot. `internal/templateinstall` revalidates the pinned source before creating resources. Preserve both call paths when changing normalized fields or diagnostics.
- When moving template files or dependencies, update import paths, schema copy paths, fixture paths, CI template jobs, and `.github/workflows/templates.yml` path filters. Keep the public schema URL and existing manifest format stable unless the task explicitly changes them.
- Read `site/README.md` before changing publication or site API behavior. Use focused template, catalog, and installation checks for the changed contract.
