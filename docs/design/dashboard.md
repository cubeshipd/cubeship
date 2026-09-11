# The dashboard

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

`web/` is a Next.js app with `output: "standalone"`, built by
`Dockerfile.web` into its own image — `cubeship`, beside the daemon's
`cubeshipd` — and run as `cubeship-frontend`, a sibling of the daemon on
the `cubeship` network. The image and the container are named
differently on purpose: the image is what somebody pulls, and the
container is what `docker logs` names.

**The daemon is the only thing in front of it.** It answers `/api`
itself and proxies everything else to that container, so an instance is
still **one address**: Cubeship is installed with one command and
reached by IP before there is a domain, and a dashboard on a second port
would be a second thing to open, a second thing to firewall, and a
session cookie set on one would not be sent to the other. Nothing
publishes the container's port.

It used to be a static export compiled into the daemon. That bought one
binary at the cost of every route being static — no `[dynamic]`
segments, so whatever identified a resource travelled in the query
string. Four levels deep that stopped being a constraint worth paying,
and the product was bending around a build flag. **Write real dynamic
segments.** Nothing carries an identity in the query string any more.

`internal/web` is the proxy, and the one thing it does beyond proxying
is explain itself: a dashboard container that is not up answers 502 with
the container's name and the fact that the API is unaffected, rather
than a bare gateway error at the address the installer told you to open.

`make web-dev` runs the dashboard on :3001 with hot reload, and a daemon
on the host proxies there rather than to a container —
`bootstrap.FrontendAddress` is the one place that branches. Reaching
:3001 directly works too; it rewrites `/api` to the daemon.

**A page's params come from Next's generated route types**, not from a
hand-written one: `PageProps<"/projects/[project]/[env]/[app]">` reads
the segments the directory actually has, so a segment renamed without
the page following it is a type error. A hand-written type is how a page
went on destructuring `org` after organizations were removed and asked
the daemon for `/apps/undefined/<project>/<env>/<app>` — which answers
"no such endpoint", at the address the dashboard sends you to after
creating something. Those generated types do not exist in a fresh
checkout, so `pnpm typecheck` writes them first; CI runs that before the
build.

Package manager is **pnpm** (`pnpm-lock.yaml` is the lockfile the image
build installs from, along with `pnpm-workspace.yaml`, which carries the
settings that install is governed by), and linting and formatting are both **Biome** —
`pnpm lint` checks, `pnpm format` writes. There is no ESLint and no
Prettier.

### Two layers, and the sidebar says so

The sidebar is in sections, because its entries are not peers:

- **Overview** — `/`, and no section of its own: it is the instance
  itself, above everything that is in it.
- **Workspace** — projects, environments, apps. What you deploy.
- **Platform** — credentials, registries, Git providers, DNS providers,
  certificates, the firewall, the machines this instance is made of, the
  instance itself. What the instance is wired to. Nothing in
  it belongs to a project, and almost none of it is touched twice: a
  registry is connected once and deployed through for a year.
  **Credentials is first**, because the others stand on it.
There is no **You** section any more. It held one item, and what is
yours — how you sign in, what colour you see, and who else can get into
this instance at all — is under your own name at the foot of the
sidebar rather than filed beside what the instance is made of.

Flat, those read as one list of peers, and "Registries" sat beside
"Projects" as though choosing between them were a normal thing to do.
A new module goes in whichever section it belongs to; if that is not
obvious, it is worth deciding before writing it.

The **URLs are unchanged** — `/registries`, `/dns`, `/settings` stay
where they are. The layer is a fact about what a thing is, and moving
every platform page under a `/platform` prefix would break links people
already have for no gain the sidebar does not give.

### How the dashboard is navigated

`/` is the **Overview**, and it is the only screen about the instance
rather than about something in it: what is on it, what the machine is
doing, and what every container on it is using. It is
what you land on because it is the question you have before you know
which project you want — the projects grid was that address for as long
as there was nothing else to land on, and it says nothing about whether
the box is out of disk.

Then four levels, and only the first is in the sidebar:

```
project        /projects                          the grid
  environment  /projects/<project>/<env>          tabs inside a project
    app        /projects/<project>/<env>/<app>
```

The URL **is** the app's reference, and the project's and the
environment's are its prefixes. `/projects` is the address that grid was
always the index of. Everything lives under `/projects`
because an app only means something inside an environment — a top-level
`/apps` would be a section for something that has no meaning on its own.
`/projects/<org>/<project>` redirects to `production` rather than being
a screen of its own, so there is one page for "a project's apps" instead
of two that have to stay identical.

`settings` is refused as a slug for any of them (`slug.Reserved`).
Next.js resolves a static segment before a dynamic one, so an app
actually called `settings` would be a resource nothing could open — the
settings screen would answer at its address instead, silently. Refusing
the name at creation is the only place that can be caught while the
person who typed it is still there.

There is **no flat list of apps**. An app only means something inside an
environment — `gateway` is unique in `acme/api/production` and nowhere
else — so a page that listed every app on the instance was listing
things whose names do not identify them. A project opens on
`production`, the environment it always has and cannot lose.

DNS and registries follow the same shape:

```
/credentials                    the secrets, and where one is renamed
                                or rotated

/dns                            the providers, and which secret each
                                writes through
/dns/[id]                       one provider's zones
/dns/[id]/zones/[zone]          a zone's records, by domain name

/registries                     the logins, Cubeship's own first
/registries/[id]                what one holds
/registries/[id]/settings       which credential it logs in as,
                                deleting it
```

`/dns` has no `[id]/settings`, and that is the model showing through:
**a DNS provider has almost no configuration of its own.** It is which
API to speak and which stored credential to speak it with, so `/dns`
lists `GET /dns` and adds, re-points and removes rows in place.

Adding one asks two questions — the provider, and the credential — and
the second offers "type a new one", so a first provider takes no trip to
the Credentials screen. It does **not** send you there to make one
first. For one release it did, and that was the whole idea upside down:
see below.

`cubeship` is the reserved id for the registry this instance runs.
Every other id is a stored credential's, which is a number, so the two
cannot collide — and a link to Cubeship's own registry is a name rather
than a blank.

A zone is addressed by its **name**, not by the provider's id for it: a
name is what someone recognises in a link they were sent, and it is
unique within an account either way. The id is what every API call
needs, so it is resolved from the zone listing on arrival — one extra
request in exchange for a URL that says what it is. A credential keeps
its numeric id, because its label is a thing its owner renames.

A project and an environment each have a **settings screen** (`/projects/<org>/<project>/settings`)
rather than a delete button in its header: renaming it, describing it and
destroying it are not the same kind of act, and the last one belongs at
the bottom of a page you went to on purpose. An environment's screen is
the same three sections, reached from the tab row inside a project.
`production` is the one row whose delete button is disabled rather than
absent — the reason it cannot go is worth reading, and a missing button
explains nothing. `ConfirmDialog` guards every
irreversible action by making you type the thing's own name — a second
button is no obstacle to a misclick, and the dangerous case is precisely
the one the daemon would happily carry out. **Anything that deletes or
unlinks goes through it**, wherever it lives: a table's trash icon, a
"revoke" beside an API key, unpublishing a database's host port.

**Nothing has a display name. A slug is a name.** Creating anything asks
for a slug and nothing else, and that is all there is afterwards.

An app was always this way — its name *is* its slug, the last component
of its reference — and projects and environments having a
second, editable name made the rule an exception rather than the rule:
two ideas for one thing, asked for at creation, drifting apart after.

The derivation is what settled it. `slug.Title` turned `public-api` into
`Public Api`, deliberately dumb because anything cleverer would be a
dictionary — so the name was, almost always, the slug spelled worse.

The **description** survived that cut and has now gone the same way.
It was kept on the grounds that it says something a name never could,
which is true and was not enough: it is optional, asked for once at
creation, read by somebody who already knows what the thing is, and the
only screen that ever showed one was a project card. What stood behind
it was a settings section per level whose reason to exist was editing
it — three forms, three PATCH bodies, three MCP arguments — for a
paragraph nobody writes twice.

So `name` and `description` are both gone from every create body, every
PATCH, every MCP tool and the CLI, where the positional argument is the
slug. **A project and an environment now have nothing that can be
changed at all**, which is why neither has a PATCH endpoint any more:
their slugs are fixed, their variables have their own endpoints, and an
endpoint that can change nothing is a promise to whoever calls it that
something can be. An app keeps its PATCH — its source, its ceiling and
where it runs are all still decisions.

**No slug is editable after its resource exists** — project,
environment or app. Every one of them is a path component of an
app's registry reference, and that reference is derived on read rather
than stored, so renaming any of them would silently move every app
underneath: pushes configured against the old path would start failing,
and images already pushed would be stranded where nothing looks for them
again and no garbage collection reclaims them. The identifier is the one
promise the daemon makes to whatever is configured against it.

The slug is **not shown read-only on a settings screen** either. It was,
with that reason under it, and a value you cannot edit on a page you
opened to edit something teaches people that settings screens are where
facts live. It is in the URL and in the header of every page under it.

### An app is created empty

Creating an app asks for a slug, in a modal, like every other
resource. It arrives with **no domain and nothing chosen**,
and it deploys anyway.

**An app with no domain is a normal app**, not a half-finished one. A
worker, a queue consumer, a service its neighbours call by container
name — none of those should answer on the internet, and an instance that
could only run things that do would be the wrong instance.
`traefik.Labels` already says so: with no domains it emits the network
label and no `traefik.enable`, so Traefik is given no opinion rather than
an empty rule. `Orchestrator.Start` used to refuse the deploy, which was
this rule read backwards.

Where an app is served is still a decision with consequences, made in
the app's settings rather than guessed at in the moment you name it, and
`PATCH /apps/{ref}` is the only place any of it can be changed.

The source and its settings are one field group there, never four
independent ones: `checkOrigin` judges them together, and moving an app
onto a source that builds re-checks the role against the source being
moved *to*, because that is the decision being made.

### Two sources, not four

The daemon has four: `registry`, `external`, `dockerfile`, `railpack`.
The dashboard shows **two**, because there are only two things an app
can be — something this instance builds, or something someone else
already built — and the four are two answers to a second question:

```
GitHub          ─┬─ Railpack     railpack
                 └─ Dockerfile   dockerfile
Docker image    ──  which registry?  ──  Cubeship's → registry
                                         anything else → external
```

Flattening them into one list of four put "how it is built" beside "what
it is", and made the choice that decides whether a `docker push` deploys
the app look like a peer of the choice between two build tools.

The form mirrors the daemon's `checkOrigin` refusals inline — a tag on
an external image, an `ssh://` repository, a `#ref` in the URL — so a
mistake is a sentence under the field rather than a rejected submit. It
is a courtesy, not the rule: the daemon still checks, and it is the one
that decides.

### Which registry, which image, which tag

`components/image-source.tsx` is the Docker-image half, and it is three
questions in the order somebody answers them rather than a choice
between two named things.

It was a pair of cards — Cubeship's registry or another one — above a
field you typed a reference into. The pair was the *model* showing
through rather than a decision anybody has: "another registry" is not
one thing, it is however many this instance is connected to, and picking
it told you nothing about which. `external` is still what the daemon
stores for every one of them, because the only thing it has ever needed
to know is whether it runs the registry itself.

**Every one of the three is listed.** The registry from `GET
/registries`, the image from that registry's own catalogue, and the tag
from the repository — newest first, and the newest **chosen**, because
an empty tag is not a neutral state: it means `latest`, which is a
different decision from the one somebody is in the middle of making.

Two places it degrades, and both are ordinary rather than broken:

- **Docker Hub has no public catalogue**, so the image is typed there
  and only the tag is listed. It is in the selector whether or not
  anything is connected — a public image needs no login, which is the
  one thing a fresh install can run, and a selector that could not
  express that would have taken the feature away.
- **A registry may refuse its own catalogue** (`ErrNoListing`, 501).
  Same shape, same fallback.

`GET /registries/tags?image=` is what makes the first of those work: it
takes the reference rather than a registry id, because the registries an
app may pull from are not the set this instance has rows for. It is a
**member's**, unlike listing what a stored registry holds — that is the
instance's inventory of what it is wired to, and this is one repository
somebody already named.

**On Cubeship's own registry there is no image to pick.** It is the
app's own path, shown and disabled with that reason: a push is matched
to an app by its reference and nothing else, so pointing one app at
another's path would deploy the wrong app on every push.

**And the tag is the autodeploy switch.** See "Where an app's image
comes from" — `source_tag` empty is what "deploy on push" *is*, so the
switch and the tag field cannot both be answered, and the form hides the
second when the first is on.

### A log is coloured, and the colour is the program's

`components/ansi.tsx` reads the terminal escapes an app writes and
renders them, rather than printing the codes that produce them. Without
it the line somebody is trying to read arrives as
`[2m2026-…[0m [32m INFO[0m`, which is the one field on the screen made
unreadable by the thing that was meant to make it clearer.

Stripping them was the other answer and it throws away a decision the
author made: in a log the colour of the level is half of how it is read.
So the sequences are parsed, and what is not understood is **dropped
rather than printed** — an unknown code is noise either way, and noise
nobody can see is the better kind.

**The eight colours do not follow the palette**, and they are the only
ones here that do not. `--ansi-*` is defined once in `globals.css`: an
app writes red because something failed, and a red that came out pink
under the pink theme would be the interface overruling what the program
meant.

It reads SGR and nothing else. A log is a stream of lines rather than a
screen to be addressed, so cursor movement and erasure would mean
nothing here even if they were honoured — they are consumed and
discarded, which is what keeps a stray one from being printed in the
middle of a word.

**The filter and the download see the text with the codes taken out.** A
filter over the raw bytes answers "no line matches" for a word plainly
on the screen, and a downloaded file full of escapes is one an editor
cannot show. The panel is the only place the colour is real.

### The components

`src/components/ui/` is [shadcn/ui](https://ui.shadcn.com) over Base UI,
generated by `shadcn add` and themed only through the CSS variables in
`globals.css` — nothing else in there is worth hand-editing, and Biome's
linter is turned off for the directory for that reason.

Two things those primitives expect that nothing else supplies, and both
live in `globals.css` because that is where what they need is declared:

- **`data-horizontal` and `data-vertical` are custom variants.** Base UI
  writes `data-orientation="horizontal"`, and Tailwind reads a bare
  `data-foo:` as the attribute `data-foo` — so without the two
  `@custom-variant` lines the orientation rules in `ui/tabs.tsx` match
  nothing, and a tab panel renders *beside* its own tab list.
- **`TableCell` carries `whitespace-nowrap`.** A column that declares
  `wrap` therefore has to say `whitespace-normal` as well, which
  `DataTable` does: `break-words` alone changes where a line may break
  and not whether it may, so every wrapping column quietly ran off the
  side instead — a build's error, a DKIM record's value.

**Tabs come in two looks, and which one is not a preference.** The
generated default is a filled box with a lighter box inside it for the
tab that is on, and it is right for the environment switcher inside a
project: a row of slugs beside a `+` and a gear, which is a control
rather than a heading. `variant="line"` is the other, styled unlayered
in `globals.css` — uppercase labels on a 1px rule with the live one lit
cyan — and it is for tabs that stand above sections and name them, like
an app's. A filled box there drew more of itself than the thing it was
switching.

`src/components/` is the layer above it, in the vocabulary of this
product rather than of a component library: `Shell`, `PageHeader`,
`StatusBadge`, `TextField`, `ActionButton`, `ErrorAlert`, `Notice`,
`ValueCard`, `RowActions`, `AuthLayout`. A page composes those and
reaches for `ui/` directly for the rest. **A page should not restyle a
primitive** — if two pages need the same thing to look the same, it
belongs in `src/components/`.

`RowActions` is the one at the end of a table row, and it exists
because the buttons were being written out per page and the difference
showed: some lit up under the pointer and some did not, which reads as
"this one is not a button". The hover is the whole affordance — an icon
on its own says nothing about being pressable — so it is decided there
rather than by whoever writes the next table. `danger` is the only
colour a row action spends, which is what makes it mean something when
it appears.

**A page title carries no paragraph under it.** `PageHeader` has no
`sub`: a title says what you are looking at, the screen itself is what
explains it, and a paragraph repeated on every visit is a paragraph
nobody reads twice. `SectionHeader` keeps one, because a section's
subtitle is about the specific thing under it.

**A choice between named things is a select**, not a grid of cards —
which provider, which engine, which credential. `OptionCards` is for
the case it was built for and no other: where picking wrong is
expensive and the difference is a sentence rather than a word, which is
where an app's image comes from and how it gets built. Everywhere else
the cards were a paragraph per option in a dialog nobody reads twice.

### Your settings, and the instance's

`/settings` is the **instance's** — its domain, its contact address, when
it updates itself. `/account` is **yours**, and it is tabs: how you sign
in, what it looks like to you, and, for an admin, who else can get in at
all. Reached from the menu under your name, not from the sidebar.

**Who can reach the instance is not in there.** `/users` is in
**Platform**, beside the credentials and the machines, because it is a
fact about the instance and the same kind as which registry it pulls
from — your own password and the colours you see are the other thing.
Every endpoint behind it — `POST /users`, `GET /users`, `DELETE
/users/{username}`, `DELETE /users/{username}/credentials` — existed
from the start with nothing in the dashboard reaching them.

It is an **admin's screen, reads included**: the list says who holds a
way in, which is not something a member needs and is exactly what
somebody probing would want, so a member is sent away rather than shown
an empty table. **Adding somebody is above the table**, because that is
what brings anybody to the screen — the table answers "who is there",
and you already know when it is only you.

The two refusals the daemon makes are said before the click rather than
after it: the account you are signed in as, and the last admin.

**Adding someone hands back a password**, once — this instance keeps
only its hash, like every other credential here — and no API key. It was
the other way round for a release, and the account it made could not
sign in anywhere: a key is what a CLI or an MCP client carries, the
dashboard wants a session, and nothing here lets a new person set a
first password. There is no invite mail on a box like this and no reset
flow, so an admin created somebody an account, handed them a credential,
and the credential opened nothing they had been given the address of.

It is the answer setup already gives the first account, for the reason
given there: the way in is the password, and a key nobody is ever shown
is a live credential lying around for nothing. Keys stay self-service.
The password is generated unless the request names one — a field
somebody has to fill in is a field somebody fills in badly, which is the
bargain a datastore's password already makes — and whoever it belongs to
changes it from their own account screen, which ends every other session
it holds.

The table is `DataTable` and the role is `SearchableSelect` with
`searchable={false}`, which is not decoration: they are the components
every other listing and every other choice here already uses, and a
hand-rolled `Table` beside a bare `Select` is how one screen ends up
with a control a different height from the field next to it.

### The look

Cyberpunk console: near-black surfaces with a blue cast, **every corner
square** (`--radius` is `0px`, and the whole `--radius-*` scale is zeroed
so the shadcn primitives square themselves rather than being overridden
one `className` at a time), cyan as the interface accent and magenta as
the brand's second light. Depth comes from 1px lines and glow, never
from shadow. The only round things left are status dots, which read as
indicator lamps.

Cyan and magenta are both outside the range where green, amber and red
already mean *state* — running, deploying, failed — so an accent never
competes with a status on screen. `StatusBadge` is the only place a
state is turned into a colour.

Type is **Chakra Petch** for the interface and **JetBrains Mono** for
anything you would type or compare — references, images, hosts,
commands. Both are vendored as woff2 under `web/src/fonts` and loaded
with `next/font/local`: the image build already needs the network for
`pnpm install`, and a second place a build can fail is one too many.

**Everything a field looks like is decided in `globals.css` and nowhere
else** — face, surface and focus, in one unlayered block. Every field is
mono, because what goes in one here is read character by character.

Unlayered is the load-bearing part. A Tailwind utility beats an `@layer
components` rule whatever its specificity, and the shadcn primitives
ship their own conflicting classes: `bg-transparent` and
`dark:bg-input/30` on Input, `ring-3` on its focus state, neither on
Textarea. Layered, the house style lost, and a text box and a text area
in the same form came out different colours with different focus
weights. The cost is deliberate: a per-usage `bg-*`, `font-*` or focus
ring on a field no longer takes.

Buttons, badges, field labels and table headers are uppercase with wide
tracking. **That is applied in `globals.css` through the primitives'
`data-slot` attributes**, not by editing `ui/` — which is what lets a
re-run of `shadcn add` overwrite those files without taking the house
style with it. The `hud-frame`, `bg-grid`, `bg-scanlines`, `text-glow`
and `neon-edge` utilities live there too.

The dashboard is dark and only dark: `<html>` carries `dark` rather than
following the system, because the shadcn primitives carry `dark:` rules
and a visitor whose OS is light would otherwise get half of them.

**There are seven palettes and every one of them is dark.** That is a
decision rather than an omission: this is a console for a machine, read
beside a terminal, and a light one would be the only screen on that desk
that is. A palette changes **colour and nothing else** — the layout, the
type and the square corners are the product, and a theme that moved
those would be a second interface to keep working.

Each one moves the surfaces with the accent rather than leaving them
blue. A red accent on a blue-cast near-black reads as two themes
fighting, which is what makes a recoloured interface look recoloured.

The choice is a row on `users`, not a value in a browser: it is a fact
about the person, and one admin seeing two different interfaces on a
laptop and a desktop is the kind of small wrongness nobody reports and
everybody notices. The browser keeps a copy, and `ThemeBoot` writes it
onto `<html>` from an **inline script in the head** — an effect runs
after the first paint, which is the flash it exists to prevent. The
daemon serves the list of names it will accept, so a second list here
could not disagree with it. And there is no username on
`PATCH /users/me`: a preference somebody else can change is not a
preference.

The swatches on that screen are painted from **literal colours**, not
from the CSS variables, and they have to be: all seven are drawn while
one palette is live, and a variable would make every one of them the
colour of the current one.
