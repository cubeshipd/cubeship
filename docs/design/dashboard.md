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

### One table, and a reason behind a button

`/certificates` was two tables — "Issued" above "Waiting" — and that
split the list by an answer rather than by a question. What somebody
comes here to find out is whether a name is served, which is a column:
split, a name moved between tables the moment it got a certificate, and
a name in neither was one you had to notice was absent.

One `DataTable` now, every name this instance routes, state in a column
(`valid`, `expiring`, `expired`, `unused`, `waiting`, `elsewhere`).

**The explanation is behind a row action, and only on the rows that have
one.** Each reason used to be a paragraph in a table cell, which is a
paragraph nobody reads on a screen they opened to find out whether
something is wrong — and it was in every row of a table where most rows
had nothing to explain. Behind a button it can be a sentence and the
button itself is the signal: this row has an answer the others do not.

What the sentence says is what to do, and then stops. Traefik's own line
goes under it when there is one, because it is the only place an ACME
refusal is written down.

### Two halves of one box

`components/command-palette.tsx` is both: **`Cmd/Ctrl+Shift+P` is what
to do, `Cmd/Ctrl+K` is what to open.** They are different questions and
their answers do not belong in one list — "New database" and the
database called `pg` would sit beside each other under `d`, and picking
the wrong one is either a form you did not want or a screen you did not
want. `>` at the front of the query switches to commands, which is the
convention and costs one character.

**Search** is every destination: a screen, a project, an app, a
database, a store, a registry, a DNS provider.

**A command opens a form, and never does anything irreversible.**
Nothing in it deploys, deletes or provisions — a fuzzy search with an
irreversible act at the end is a way to press the wrong button quickly,
and everything irreversible here is deliberately behind a screen you
went to and a word you typed.

**A command is a link.** Every create form lives on the screen its
result lands on, holds that screen's state and closes back onto that
screen's list, so the command navigates with `?new=1` and
`useOpenOnArrival` has the screen open what it already has. The
parameter is stripped as it is read: left in the URL it would be a link
that reopens the dialog every visit, and a URL is the thing people
bookmark and send. Owning a second copy of seven forms would be seven
things to keep in step for nothing.

There is no **New app**, and that is the one absence worth stating: an
app is created inside an environment, and from a palette there is no
environment to create it in. Offering it would mean choosing one on
somebody's behalf.

**Its screens come from the sidebar's own list**, flattened. A page
added to one would otherwise be missing from the other, and the palette
is the half nobody notices is missing.

**The whole catalogue is fetched when it opens** and matched in memory —
one VPS, a handful of each thing, six requests in parallel, and a
failure is an absence rather than an error: a palette that will not open
because the DNS list timed out is slower than the sidebar.

**Subsequence, not substring**, which is the difference between a
palette and a filter: `wpa` finds `web/production/api` and nobody types
the slashes. The score puts the intended answer on top — letters in a
run beat the same letters scattered, a letter after a separator beats
one mid-word, and a short name beats a long one that matched the same
letters. What is matched is not always what is shown: an app reads as
`api` beside `web/production` and matches on its whole reference,
because the letters have to be in one string and in that order.

The shortcut is read off `event.code`, the physical key. With Shift
held, `key` is the shifted character, and on another layout it is not
`P` at all.

### The rail above every screen

`components/header-rail.tsx` is the strip the Shell puts above every
page: where you are on the left, and whatever the screen wants there on
the right.

**The path came out of the pages.** Thirteen screens each rendered their
own `‹ databases` link above the title, and the rest rendered nothing —
so "where am I, and what is next to me" was answered differently on
every one of them, or not at all. The Shell is the one place that can
answer it once, from the URL, which is already the reference.

It does **not** replace `PageHeader`. The title and the actions stay
where they are, and on a settings screen the two say different things:
the path is the app, the title is "App settings". A rail that swallowed
the title would have had to swallow the buttons beside it too.

**Every crumb looks like every other crumb**: mono, one size, written
the way the URL writes it, with the last one lit and the rest not. It
was three systems in one line for a day — the section uppercase, the
slugs mono, and the last one half again as large because it was
standing in for a page title — which reads as three kinds of thing
rather than as one path. Being lit is enough to say where you are
standing.

That is also why there is no table of friendly names any more. `storage`
was rendered "Object storage" and `settings` "Instance", the sidebar's
words kept in step with the sidebar by hand; what it bought was a nicer
noun in one place and a second list to forget, and what it cost was a
crumb that did not match the address it names.

**A trailing `settings` is a screen, not a slug.** Read positionally,
`/projects/web/settings` is a project in an environment called
`settings` — which duly offered a menu of environments to swap it for,
and said "Nothing else here". `slug.Reserved` on the daemon refuses that
name at creation for the same reason Next resolves a static segment
first, so the two lists say one thing from opposite ends.

**The mark is on the options, not on the crumb.** The path is a line of
words and stays one; what an icon per row buys is an edge to read down,
so a menu of slugs reads as a list of *buckets* rather than four bare
words under a chevron.

They come from `components/marks.tsx`, which is **one map for the whole
product** — the rail and both halves of the palette. Two lists of the
same facts is one list that goes stale, and the day a kind is added to
one of them the other keeps a different opinion about what a bucket
looks like. Where the sidebar already lists a thing it is the sidebar's
own icon, because a second mark for it would be a second name for it;
the four that have no sidebar entry — an environment, an app, a bucket,
a zone — are chosen to sit apart from the ones that do.

**A crumb with siblings is a menu**, and that is the point of putting
the path here rather than in a page. Reading `web/production/api` tells
you where you are; opening `production` and landing in `staging` is the
trip back through two screens you no longer take. Projects,
environments, apps, databases, stores and **buckets** have them; a
section and a `settings` do not.

The bucket's is there **whether or not the store has a second one**. A
control that appears only once there is something to switch to is one
nobody learns is there — and how many buckets a store holds is not
something you know before you look.

Two decisions inside that:

- **The list is fetched when the menu is opened, never before** — with
  one exception. The rail is on every screen and most of the time nobody
  touches it, so loading every project, environment and app on every
  navigation would be a request per screen for a menu that stays shut.
  The exception is a crumb whose path segment is an **id**: a registry
  and a DNS provider are addressed by a credential's number, so the
  crumb cannot say what it is called without the list and loads on
  sight. What the URL holds is never what it shows.
- **Switching lands on that level, not on the deep path you were on.**
  Picking another project while looking at an app does not go looking
  for an app of the same name in it — that app may not exist, and a menu
  that sometimes 404s is one nobody trusts twice. An app's own peers are
  the exception, because that list is already scoped to the environment
  you are in.

`RailPortal` is how a screen puts its own controls up there. A portal
rather than a prop threaded down from the layout: what wants the rail is
usually several components deep — a tab's toolbar, a table's filter —
and a prop would have to pass through every one of them to arrive.

It is **sticky**, because being reachable is the whole reason it exists,
and the screens where switching saves the most are the long ones: a log,
fifty environment variables, a deploy history.

### A filter belongs to the table, not to the page

`DataTable` takes a `search`: a placeholder and which of a row's words
to match. It holds the query, renders the field above itself with the
gap, answers the count, and says "nothing matches that" — which is a
different sentence from an empty list and must not be the same one.

It is there because it was written out beside the table four times and
came out four ways: three with no gap under the field and one with a
smaller gap than the rest, each with its own state, its own count and
its own word for no result. A filter over a list is the same control
every time; the only thing that differs is which of a row's words it
looks at, and that is all a caller says now.

**Flush against the table a filter reads as its first row.** It is a
control over the page — what it hides is gone from everything below —
and the gap is what says so.

Two listings write it out by hand, and both for a reason. The projects
grid is cards rather than a table. The registries table carries a row
with no data behind it — Cubeship's own, always first — which the filter
has to match too, because a filter that cannot hide the row you are
looking for is one that lies about its own count.

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

**There is no page header any more.** The rail above every screen is
it: the last crumb is the title, and what used to sit beside the title
goes through `RailPortal`. Of the thirty-one titles the old
`PageHeader` drew, almost every one was either the section the sidebar
already highlights or the last word of the path the rail carries —
said twice, in two faces, thirty pixels apart.

Two things came with it. `RailTitle` is how a screen the URL cannot
name says what it is called: a registry and a DNS provider are
addressed by a credential's numeric id, so the path segment is `4`, and
a page whose title is `4` has no title. And a `below` filter is no
longer header furniture — it filters the page, so it is the first thing
on the page.

**A title carries no paragraph under it.** `SectionHeader` keeps one,
because a section's subtitle is about the specific thing under it; the
page's own title never had one, and what explains a screen is the
screen.

**A choice between named things is a select**, not a grid of cards —
which provider, which engine, which credential. `OptionCards` is for
the case it was built for and no other: where picking wrong is
expensive and the difference is a sentence rather than a word, which is
where an app's image comes from and how it gets built. Everywhere else
the cards were a paragraph per option in a dialog nobody reads twice.

### Your settings, and the instance's

`/settings` is the **instance's** — its domain, its contact address, when
it updates itself. `/account` is **yours**, reached from the menu under
your name rather than from the sidebar, and it is four tabs: **General**,
**Appearance**, **Security**, **API keys**.

Four rather than the two it was, because "Account" had become the tab
for whatever was not a colour: a list of keys above a password form,
which are two different questions asked in one place — what this machine
can do as me, and how I get in.

**General is read-only, and that is not the mistake it looks like.** A
username is the identity every session and key is written against; a
role is an admin's to grant, and an admin editing their own would be a
lock with the key taped to it. The slug came off the project and
environment settings screens for being a fact among fields — the
difference is what a screen is for. Those configure a resource, and a
fact filed among its settings reads as a setting that will not take.
This screen is your account, and the first thing an account screen
answers is which account.

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

**There are eight palettes and every one of them is dark.** That is a
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
from the CSS variables, and they have to be: every one of them is drawn
while a different palette is live, and a variable would make all of them
the colour of the current one.

**`blue` is the one that had to be pushed away from the default rather
than simply chosen.** Cubeship's own is cyan on a near-black with a blue
cast, so a blue accent on those same surfaces would have been the
default with the accent nudged — and two entries in the picker that are
hard to tell apart is worse than not offering the second. Its surfaces
are properly navy, which is what every other palette here does with its
own hue.

**The account faces are named after these** — see `user.Avatars` — and
they share a vocabulary rather than a list. Nothing requires a palette
to have a face or a face to have a palette: they happen to line up, and
no test says they must, because one would fail for something that is
not a fault.
