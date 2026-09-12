# The site

`site/` is cubeship.dev: the landing page at `/`, the docs under
`/docs`, and `install.sh` at the root. A Next.js app of its own, built
by Fumadocs' scaffold and deployed on Vercel with `site` as the root
directory. It is not part of the daemon's image and the daemon knows
nothing about it.

## One palette, copied

The site is the product's face and wears the product's colours: the
cyan theme of `web/src/app/globals.css`, its two typefaces (vendored the
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
