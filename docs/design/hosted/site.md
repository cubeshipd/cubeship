# The site

`hosted/site/` is cubeship.dev: the landing page at `/`, the docs under
`/docs`, and `install.sh` at the root. A Next.js app of its own, built
by Fumadocs' scaffold, shipped as its own image (`hosted/site/Dockerfile`,
`make site-image`) and run as an app on a Cubeship instance — the
product hosting its own front door. It is not part of the daemon's
image and the daemon knows nothing about it.

The image is the dashboard's recipe: `output: "standalone"`, static
assets copied back beside the server, an unprivileged user, `:3000`.

The landing page and the docs are still exactly that: nothing in them
needs the network at run time, the docs are compiled in, and the search
index is built from them on the first query. `/templates` is not — it
reads `hosted/catalog`'s API, and `/api/v1/*` is proxied to the same
API for everybody else. The site has no database, no bucket, no session
and nothing it writes. See [templates.md](templates.md) for that half.

## The catalog can be down without taking the site with it

The site used to hold the registry's Postgres itself, and a database
that refused a connection took the whole process down before it served
a single request. It holds nothing now, so starting touches nothing.

The landing page and the sitemap read the catalog too — the templates
strip on `/` and every template in `/sitemap.xml` — and both degrade
rather than fail: a failed read logs once and the strip renders nothing,
the sitemap falls back to its static entries. Only `/templates` and the
pages under it are allowed to fail, and they fail fast and readable:
every read gives up after 5 seconds, the strip and the sitemap stop
waiting after 800 milliseconds and 1.5 seconds, and `error.tsx` under
`src/app/templates` shows a sentence instead of a stack trace. What the
catalog answers is cached for a minute, so a catalog that goes away is
not noticed until that minute is up.

## Visits are counted by our own Umami

The site counts page views with Umami, run as an app on the same
instance. The tracker is served from the site's own address: every page
loads `/u/script.js` with `data-host-url="/u"`, and `src/proxy.ts`
rewrites `/u/script.js` and `/u/api/send` — those two, nothing else of
Umami — to `UMAMI_URL` on each request, the way `/api/v1` reaches the
catalog. The browser never talks to another host, so a blocker that
knows Umami's domain has nothing to match, and the site reaches Umami on
the internal network rather than by a public name that points back at
the machine.

`UMAMI_URL` is Umami's internal address,
`http://cubeship-umami-production-web:3000`. Unset — `make site-dev` —
the two paths answer 404 and nothing is counted. The website id is a
constant in `src/lib/shared.ts`: it is in every page's HTML anyway.

Clicks are events. An element with `data-track="install"` is sent as the
event `install` when clicked, and each `data-track-<name>` attribute
becomes a property — `data-track-location="hero"` says which of the
install buttons it was. `ClickTracking`, mounted in the root layout,
listens once for the whole document and calls `umami.track`. Umami's own
`data-umami-event` was the obvious choice and is wrong here: on a link,
the tracker cancels the click and sets `location.href` after sending,
so every client-side `Link` it marked became a full page load.

## A product site, in the product's colours

The site keeps the product's cyan palette, Chakra Petch and JetBrains Mono,
vendored under `src/fonts`, square corners and dark surfaces. Base tokens
remain in `src/app/global.css`, mirrored into Fumadocs' `--color-fd-*`
variables. The dashboard and site are separate builds; neither imports
styles from the other.

The marketing site has its own composition in `src/app/marketing.css`:
large left-aligned headlines, a cinematic hero, the real dashboard directly
below it, and sections that explain deployment, data, clusters and MCP.
`editorial.css` carries the related reading and catalog treatment. A common
`SiteHeader` serves home, templates and comparisons; Fumadocs keeps the docs'
navigation, search and tree. Only the dark palette exists.

Sponsorship appears once in the header (inside the menu on small screens),
linking to `github.com/sponsors/lucasaarch`. It stays secondary to installation;
there are no donation overlays or repeated calls to action in the page.

### The scenes are enhancements

`InfrastructureScene` renders a static SVG poster first, then lazily imports
Three.js when the scene enters the viewport. The hero is a point-cloud cube
with travelling particles; the cluster is a network of stacked server plates
with packets moving between nodes. Both share a renderer lifecycle, but have
separate compositions. Pointer movement changes the camera angle subtly;
scrolling expands the hero's point cloud. Text and links remain HTML outside
the canvas.

The renderer caps pixel density at 1.5, resizes with its container and stops
its animation loop when hidden or outside the viewport. Reduced motion
keeps the SVG without initializing WebGL. Initialization errors and context
loss return to that poster. Unmount disconnects observers and listeners and
disposes GPU resources.

`src/images/brand/` preserves the generated campaign artwork, with original
prompts and tool provenance in its README. Both images are retained as unused
design assets. Neither is used in social cards or the landing page's scenes. The dashboard screens remain
real captures with demonstration data.

`src/lib/social-image.tsx` renders a consistent social card for the homepage,
docs and templates, using a deterministic SVG point cube, local Chakra Petch
fonts and actual page titles. The homepage image metadata is generated through
Next's file conventions. The root README banner at
`product/dashboard/public/logo/banner.png` is exported from the homepage's
`/opengraph-image` endpoint; refresh that export when changing the composition.

## The dashboard demo is the product's preview build

The showcase starts with real screenshots. Choosing **Explore the live demo**
mounts an iframe at `/demo`; no dashboard JavaScript is requested beforehand.
The section offers tour shortcuts, fullscreen, retry, close and reset. Shortcuts
send an allowlisted message to the iframe, preserving the visitor's in-memory
changes while navigating. Closing or resetting discards the simulation.

The site image builds `product/dashboard` a second time with
`NEXT_PUBLIC_CUBESHIP_MOCK=1` and `NEXT_PUBLIC_CUBESHIP_DEMO=1`. This build uses
`basePath: /demo` and `.next-demo` so it does not overwrite the installed
dashboard build. The public demo flag refuses to build without mock data.
The installed dashboard still excludes the fixture module.

`scripts/serve.mjs` runs the site and demo together, forwards shutdown signals
and stops both if either exits. Only the site port is exposed; the demo listens
on loopback port 3003. Next rewrites `/demo/*` to that process without stripping
the prefix. `pnpm dev` starts both on ports 3002 and 3003; install dependencies
in both `hosted/site` and `product/dashboard` first.

The demo has no daemon, database or session API. Changes live in each browser
document's fixture module; reload starts fresh. Uploads stay in the browser and
downloads contain labelled sample data. Unsupported actions explain that they
need a connected instance instead of trying a real API. External form submits
and example infrastructure links stay inside the demo. CSP limits network
connections to the same origin and framing to the site; the iframe cannot
navigate its parent. `/demo` is excluded from search indexing.

## The docs have an MCP server of their own

`cubeship.dev/mcp` (`src/app/mcp/route.ts`) serves Fumadocs' three
tools — `search`, `list_pages`, `get_page` — over streamable HTTP, off
the same `source`, `docsLlms` and search index the pages use. It needs
no key and reads nothing but the compiled docs, so it holds to "nothing
needs the network at run time". It is named `cubeship-docs` so it sits
beside an instance's own `/mcp`, which is a different host and a
different thing. `content/docs/mcp/docs-server.mdx` carries the command
for each client, one tab each; when a client changes its syntax, that
page changes.

## install.sh is a rewrite

`install.sh` and the CLI print `curl -fsSL https://cubeship.dev/install.sh | sh`.
`next.config.ts` rewrites `/install.sh` to the file on `master` in the
repository, so the site never carries a copy that could go stale, and a
rewrite rather than a redirect so `sh` never sees a `Location` header —
`curl` without `-L` would print HTML into a shell.

## The docs are the README, one section per page

`content/docs/*.mdx` is the README split into pages, in the README's
order (`meta.json`). When a section of the README changes, its page
changes. The API reference stays at `https://your-instance/docs`, off
the instance's own OpenAPI document; generating it here from
`openapi.json` is a second step Fumadocs can do, not taken yet.

## What CI runs

The dashboard's three steps plus the unit tests, under the `site` job:
`biome ci`, `test`, `typecheck` (typegen first, for the same reason as
the dashboard) and `build`. None of it needs a database. `make check`
does not cover it — nothing here is Go — except `reference-check`, which
refuses a template schema that is not `product/template`'s.

`make site-dev` runs it on `:3002`, which is also what the preview
launches.
