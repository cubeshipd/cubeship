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

## One palette, copied

The site is the product's face and wears the product's colours: the
cyan theme of `product/dashboard/src/app/globals.css`, its two typefaces (vendored the
same way, under `src/fonts`), square corners, 1px lines and glow. The
tokens are **copied** into `src/app/global.css`, once as the site's own
(`--color-background`, `--color-primary`, …) and once as what Fumadocs
reads (`--color-fd-*`). Copied rather than shared because the two are
two builds with two lockfiles; a package between them would be a third
thing to version for a dozen hex values. Change one, change both.

Only the dark palette exists here. `dark` sits on `<html>`, next-themes
is off in `RootProvider`, and the switch is off in `baseOptions` — a
light version of this site would be a design of its own, not a toggle.

`hud-frame`, `bg-grid`, `text-glow`, `neon-edge` and `label` are the
dashboard's utilities, re-declared. A section on the landing page is
built the way a section of the dashboard is: a mono label, a title, a
line under it.

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
