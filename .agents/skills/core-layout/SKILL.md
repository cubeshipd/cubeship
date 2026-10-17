---
name: core-layout
description: Organize or refactor Go code in Cubeship core, including package ownership, large files, and module boundaries.
---

# Core layout

Use this when changing the structure of `core/` or adding a substantial Core behavior.

- Keep executable entry points in `cmd/`, repository tools in `tools/`, integration tests in `test/`, and all application packages in `internal/`. `internal/platform/` owns external mechanisms; domain packages own entities, persistence, use cases, and adapters for their concept.
- Trace every caller and import before moving a package or symbol. Preserve API, CLI, MCP, migration, fixture, build, and workflow paths that refer to it. Avoid a new package when cohesive files in the existing package keep the dependency direction clear.
- Put business decisions in a domain service. HTTP and MCP adapters parse, call the service, and render. SQL belongs in the domain repository. Wire cross-domain dependencies in `internal/server` through interfaces declared by the consumer when a lower layer needs a higher one.
- Keep each maintained Go source, test, and hand-edited schema file at 600 lines or fewer. Machine-managed files such as `go.sum` follow their own format. Split at a coherent responsibility within the same package; name files for the use case or adapter slice. Do not create one-function files solely to satisfy the limit or hide complexity in another oversized file.
- For a structural change, run the same focused proof before and after, then `make check` before committing. Record anything that needs Linux Docker or live runtime separately.
