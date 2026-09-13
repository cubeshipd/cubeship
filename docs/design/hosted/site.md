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
and its API under `/api/v1` read Postgres, and photos read a bucket.
See [templates.md](templates.md) for what that half of the site is and
how it stays out of the way of the half that is still static.

## Postgres can be down without taking the site with it

`register()` used to `await` the migration runner, so a database that
refused a connection took the whole process down before it served a
single request — an operator running `make site-dev` against a
misconfigured `DATABASE_URL` got a crashed server instead of a working
landing page. Migrations now run in the background: `runMigrations` is
still one attempt and the real error, for `withDatabase()` in tests to
await against a database that is already up, but `register()` calls
`runMigrationsInBackground` instead, which never throws — it logs the
failure and retries with a backoff capped at 30 seconds until one
attempt applies cleanly.

The landing page and the sitemap read Postgres too — the templates
strip on `/` and the published-templates list in `/sitemap.xml` — and
both degrade rather than fail: a failed query logs once and the strip
renders nothing, the sitemap falls back to its static entries. Only
`/templates`, `/u/*`, `/me/*`, `/admin/*` and the `/api/v1` routes that
read the database are allowed to fail, and they fail fast and readable:
the pool gives up connecting after 3 seconds and a query after 5,
rather than waiting out the OS's ~75-second TCP timeout, while the strip
and the sitemap stop waiting for their own read after 800 milliseconds
and 1.5 seconds, so a dead database costs the landing page under a
second. The pool's limit is not that short because a remote database's
handshake alone can take longer than the landing page may wait. `fail`
in `src/lib/http.ts` maps a connection failure to a 503 with the code
`unavailable` in the one place every route's errors already go through,
and `error.tsx` under `src/app/templates` catches what a Server
Component throws and shows a sentence instead of a stack trace.

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

The same three steps as the dashboard, under the `site` job: `biome ci`,
`typecheck` (typegen first, for the same reason as the dashboard) and
`build`. `make check` does not cover it — nothing here is Go.

`make site-dev` runs it on `:3002`, which is also what the preview
launches.
