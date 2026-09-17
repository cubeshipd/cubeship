# The OpenAPI document

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../../AGENTS.md).

Served at `/openapi.json`, with a Scalar reference at `/docs`. Both are
unauthenticated, because Scalar fetches the document from the browser
with no credentials to offer.

Scalar is loaded from a CDN, **pinned and with an integrity hash**. The
version says which file to ask for; the hash says what the file is, and
they are not the same promise — a CDN serving something else under that
version would run its code on this daemon's own origin, beside the
session cookie. `scalarIntegrity` is how to change it. The page's CSP
allows no inline script: it has none of its own, and Scalar renders
without one.

**The document is the product's API, not an inventory of routes.** It
describes what someone integrating against Cubeship would call:
projects, environments, apps. It leaves out the daemon's
own machinery (`/healthz`, `/openapi.json`, `/docs`, `/mcp`), the
two shell sessions (a WebSocket is not a request with a response — see
[shell.md](shell.md)), the
registry's two endpoints (`docker` and the registry container call
those, nobody else), and API-key self-service, which you do once from the
CLI.

That exclusion is declared, never implied. `httpx.Router` has two
registration methods: `Handle` for a documented route, and
`HandleInternal` for one deliberately left out. A documented route with
no operation, an operation with no route, and an internal route that
*is* documented all fail a test — so the trimming cannot decay into
forgetting. `TestDocumentedSurfaceIsTheProductAPI` pins both lists, so
moving an endpoint between them is an edit someone made on purpose.

It is hand-written, module by module, in each `openapi.go` — not
generated from annotations. Every operation needs a unique
`operationId`, a summary, a tag, and its path parameters declared.

Error responses are `text/plain`, because that is what `http.Error`
writes. `openapi.Unauthorized`, `.Forbidden`, `.NotFound` and
`.BadRequest` carry the shared wording — including why 404 and 403 mean
different things.

## An MCP tool's output schema is always an object

The SDK infers a tool's output schema from the type its handler returns,
and a Go slice infers as `{"type": ["null", "array"]}`, because a nil
slice is JSON null. The MCP schema says an output schema is an *object*
schema, and a client that validates `tools/list` against it — anything
built on the Python SDK's pydantic models — rejects the **whole list**
over one tool that isn't. Every tool vanishes at once, and the error
names only an index.

So a handler returning many of something returns `mcpx.List[T]`, whose
one `items` field holds them, and `mcpx.Of` turns a nil slice into an
empty one on the way in. `TestMCPToolOutputSchemasAreObjects` walks every
registered tool; `internal/mcpx` pins the inference itself, with no
database to start.
