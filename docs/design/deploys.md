# Deploying, and building

Design notes for Cubeship. The conventions every change follows are
in [CLAUDE.md](../../AGENTS.md).

## Where an app's image comes from

Every app carries a `source`, and a value the daemon cannot act on is
refused at creation — accepting one would let someone create an app that
can never deploy.

- **`registry`** — pushed to Cubeship's own registry. The push is what
  deploys it. Needs a domain before there is anywhere to push.
- **`external`** — an image in a registry Cubeship does not run. Nothing
  notifies Cubeship when one of those is pushed to, so **there is no
  autodeploy**: a deploy is something you ask for. It needs nothing
  configured, which makes it the one thing an instance can run the minute
  it is installed.
- **`dockerfile`** — built here, from a Dockerfile in a Git repository.
  BuildKit does the clone, so nothing needs git on the host.
- **`railpack`** — built here from a Git repository with **no Dockerfile
  at all**: Railpack reads the code and works the build out.

Both builds autodeploy once this instance has connected the GitHub
account the repository is on. Until then, and for a repository anywhere
else, a deploy is something you ask for.

A building app stores `source_repo`, `source_ref` and
`source_dockerfile`. The repository and ref are not Dockerfile-specific —
anything that builds from a repository needs them — so they are not named
for it. A deploy's tag argument overrides the stored ref, which is how
"deploy this branch" works.

The repository must be `https://`, `http://` or `git://`. That rule is
about what the builder can fetch **unaided**, not about what is safe: ssh
needs a key this instance does not have, and a clone failing on a host
key deep inside a build explains nothing. Only https authenticates what
comes back, and a build runs whatever comes back.

An external app stores `source_image`, the reference minus the tag,
because it has nothing to derive one from. The tag is the deploy's
argument, so an image given with one is refused: an app pinned to a tag
could never be told to run another. A registry app naming an image is
refused too, rather than silently ignored.

**Which tag an app runs is `source_tag`, and empty is a decision.** On
Cubeship's own registry it means "whatever is pushed" — the push is the
deploy, which is what every app has done since there were apps — and on
any other registry it means `latest`. A tag in it is the app pinned:
deploys happen when somebody asks, and a push is ignored even when it
carries that very tag, because an app pinned to `v1.0` is one somebody
decided should run `v1.0` and moving it from a notification nobody saw
would overrule that invisibly.

So **autodeploy is not a column**. It is `source_tag == ""` on an app
whose source is `registry`, reported as `autodeploy` on every response
and derived on read. A flag beside the tag would be two settings that
can contradict each other, and one of them would have to lose in
silence; this is the same shape as `autoscale_max = 0` being what
autoscaling off is. A building app has no tag at all — its version is
its ref — and `checkOrigin` refuses one.

The deploy's own argument still wins over it, which is what `cubeship
app deploy --tag` is: what was asked for beats what was configured, and
the deployment row records the tag that actually ran rather than "the
one configured at the time", so the history can still answer which
version was live last Tuesday.

A push under an external app's name does not deploy it — the webhook
checks the source. Our registry will accept the push, since the
repository path exists either way, but running an image because something
unrelated landed under that name would deploy a version nobody asked for.

The seam is `ImageSource`, in `internal/app/source.go`, and the split is
the point:

- **`Check`** is cheap and runs before a deployment row exists, so a
  misconfiguration is a refusal the caller sees rather than a deployment
  that fails minutes later with nobody watching.
- **`Resolve`** produces the image, inside the detached deploy. A source
  that builds will build there — nobody is holding a connection open for
  it, and the deployment row is where the outcome goes.

`Orchestrator.Start` takes a tag, not an image reference: which image a
tag names is the source's answer. `deployments.image_ref` holds what was
asked for until `Resolve` says what actually ran.

## Building images

`internal/platform/buildkit` turns a directory of source into an image,
through a `cubeship-buildkit` container.

**The result is loaded into the Docker Engine unless the app runs
somewhere else.** On one VPS the image never has to leave the box, and a
registry round-trip would need credentials, a reachable host and a
certificate — three things that can all be missing on a fresh install.
`dockerx.LoadImage` imports the tarball BuildKit writes, streamed
through a pipe so a whole image is never held in memory.

An app placed on another machine is the case where it does have to
leave. Then the exporter is `image` with `push=true` instead of
`docker`, and **nothing comes back here at all**: no tarball is written,
no pipe is opened, and the bytes go from the builder to the registry
without passing through this process. The machine that will run it
pulls from there. `Orchestrator.buildTarget` is where the two answers
are decided, from one fact — which machine the app is on.

**The builder has a credential of its own**, `app.BuilderUsername` and
the `builder-token` the daemon generates on first start. Not the webhook
token, which would widen it from "forge a push notification" to "push
any image", and not a person's API key, which would put somebody's
credential inside every build. `pushAuth` answers for one host and
refuses every other, so a registry named in a Dockerfile cannot be
handed the login. The registry's token endpoint grants it push and pull
and never delete — the one action building again does not undo.

**A build still happens on the control plane, wherever the app runs.**
That is where the builder, the repository credentials and the plan are;
what crosses to the other machine is an image reference. So an app that
builds can be placed anywhere **once the instance has a domain**, and
without one it is refused — with the reason — rather than built into an
image nothing can pull.

The build context is streamed from the *daemon's* filesystem over the
client session, so the builder container needs no access to it.

Two things about that container:

- **It is privileged**, the only one Cubeship runs that way, because
  building an image means running one.
- **It starts on demand**, not at boot. An instance that only runs images
  it is given never builds anything, and a privileged container idling on
  it is cost with no return. `bootstrap.EnsureBuildKit` is what the first
  build calls; `Ensure` makes it idempotent after that.

The daemon reaches buildkitd over a **unix socket** in a host bind mount,
not a port: it is a build service running as root with no authentication
of its own, so filesystem permissions are the guard and there is no port
for a misconfigured firewall to expose. `Builder` takes an address, so
its tests use TCP instead — Docker Desktop's file sharing refuses to
create a socket in a bind mount, and a socket-only test on a Mac would
prove nothing but the Mac's limits. Everything above the dial is the same
either way.

`Build` calls `ListWorkers` before solving. `client.New` does not dial, so
without it an unreachable builder arrives as a solve failure buried in
gRPC wording rather than as `ErrUnavailable`.

## The two ways of building

They differ in **where the recipe comes from**, and that difference
decides everything else.

A Dockerfile is *in* the repository, so BuildKit clones for itself and
nothing touches the daemon's disk — **through a URL ending in `.git`**,
which `buildkit.GitContext` adds. That suffix is the whole of how
BuildKit tells a repository from a file: an `https://` context is a git
remote only when its path ends that way, and anything else is
*downloaded*, with none of the credentials a clone carries. Without it
every private repository failed with `failed to read downloaded
context: ... invalid response status 404`, which names neither the
repository nor the fact that it was never treated as one. The suffix is
added at build time rather than stored, because what somebody pasted is
what the settings screen shows and `.git` is a fact about this builder. Railpack has to **read** the
repository to work out how to build it, and that reading happens in the
daemon — so it clones first (go-git, no git on the host), plans, and
hands BuildKit the result. Both then go through one `solve`.

Railpack is used as a **library**, not a binary: `core.GenerateBuildPlan`
produces the plan, and the build itself runs through BuildKit's
`gateway.v0` frontend pointed at `ghcr.io/railwayapp/railpack-frontend`.
No extra artifact to ship, and nothing to keep in step by hand except the
version — which is exactly what
`TestTheFrontendMatchesTheRailpackWeBuildPlansWith` pins. A plan is a
versioned document; a frontend reading one it does not understand fails
for no reason anybody can see.

`GenerateBuildPlan` reports a repository it cannot plan through
`Success: false`, not through `err` — err is for transient failures. Its
logs are what tell someone their repository is missing a start command,
so they become the error rather than being dropped for "planning
failed".

**The app's environment goes into the plan**, not only into the
container. Railpack reads it for the versions and commands a project pins
(`RAILPACK_NODE_VERSION`, a build command), so two apps on one repository
with different environments are two different builds.

Mount caches are keyed per app (`cache-key`), because two apps sharing
one would fight over it.

**Nothing prunes the build cache.** It lives in the data directory and
grows with every build. The registry's own disk has a pass somebody can
press — see "Deleting" — and this has nothing at all:
`docker exec cubeship-buildkit buildctl prune` is the manual answer for
now.

## Acting as a GitHub App

One App per instance, registered by whoever runs the VPS; its
credentials are settings. The App is installed on the GitHub accounts
whose repositories this instance may build, and an installation is a row
here — the instance builds what it has been given access to, and nothing
else.

**The App is public, and connecting an installation is verified.** The
two go together. A private GitHub App can only be installed on the
account that owns it, so an instance whose App was private could reach
one person's repositories and no GitHub organization's — the install page
offered none at all. Public fixes that and costs the guarantee that came
with it: anyone can install it, so an installation id is a number the
caller chose and every id is somebody's real id.

`request_oauth_on_install` is what pays for it. GitHub sends the
installer back with a code as well as an id; `Connect` spends the code,
asks GitHub which installations *that person* administers, and refuses
an id outside the answer. The account is read from the same answer
rather than from the request — it is what every repository lookup
matches against, and a mismatched one would silently stop matching.
Turning the OAuth off while leaving the App public would make
connecting an installation a way to read a stranger's private code.

**A delivery has to name an installation this instance connected.** The
signature already stops a forgery; the lookup stops a genuine delivery
from an App installation nobody here asked for.

**Registering the App carries a nonce, and the exchange requires it.**
GitHub's manifest conversion endpoint is unauthenticated — a code is a
code, whoever made the manifest it came from — and the redirect that
brings one back is a link a browser follows with the session cookie
attached. So `POST /settings/github/manifest/state` issues a single-use
`state` bound to the caller, the manifest form carries it to GitHub,
GitHub echoes it back, and `RegisterFromManifest` refuses a code that
arrives without it. Without that, a link sent to a signed-in admin would
make this instance somebody else's App: their webhook secret, their
private key, and installation tokens over every repository the admin then
granted it — landing them, meanwhile, on exactly the install page they
expected. The nonce also carries whether the registration may **replace**
an App the instance already has, decided before GitHub is involved
rather than read from the redirect coming back, because replacing one
breaks every installation on it.

The App's private key and webhook secret are **write-only**. `settings`
reports `github_connected`, never the values;
`TestTheAppCredentialsAreNeverReturned` pins that.

**A delivery with no secret configured is refused, not trusted.** An
endpoint that starts deploys on an unauthenticated POST is a way to make
this instance build anything. Everything else about a delivery answers
200 — GitHub retries what it could not deliver, and a payload this daemon
cannot act on is not something a retry fixes.

An app with no `source_ref` deploys on a push to any branch; naming a ref
is how you opt out. A tag is never a branch.

## Cloning a private repository

`TokenForRepository` mints an installation token, cached until shortly
before it expires — GitHub's last an hour and a clone takes seconds of
that, so minting one per clone would be a round trip against a rate limit
for nothing.

**The token never goes in the URL.** A URL appears in BuildKit's own
progress output, and that output goes into a deployment row and then into
a browser. It is handed over as:

- BuildKit's `GIT_AUTH_TOKEN` session secret, for a Dockerfile build —
  BuildKit's own name for Basic auth with the user `x-access-token`,
  which is exactly what a GitHub App token is used as.
- go-git's `BasicAuth`, for the clone a Railpack build does here.

The JWT is hand-rolled: an RS256 assertion is a header, a payload and a
signature over their base64, and a dependency for that is one more thing
to keep current for no gain. `iat` is backdated a minute, because GitHub
refuses an assertion issued in its future and a VPS clock a few seconds
fast is enough.

No installation found is **not** an error. A public repository needs no
token, and letting GitHub refuse a private one beats refusing a clone
that would have worked.

## What a build's output does

A build is the one part of a deploy long enough that watching it is the
point, so `deployments.logs` is written **while it runs**, on a timer
matched to how fast a dashboard polls — not once at the end.

`deploymentLog` buffers and flushes rather than writing per line, because
BuildKit emits output in small pieces and an UPDATE per piece would make
a noisy build heavier on the database than on the builder. It is capped
at `MaxDeploymentLogBytes` and **keeps the tail**: the reason a build
failed is at the end of what it printed. Truncation says so rather than
leaving a reader thinking they have the whole build.

`Close` writes whatever is left, and the deploy path defers it, because
that last flush is the one carrying the explanation of a failure.

**A listing carries none of it.** 256 KiB a row against fifty rows of
history is twelve megabytes, re-fetched every couple of seconds by
anything watching a build — which is exactly when somebody is watching.
`deploymentListColumns` selects `logs <> ''` instead, so a listing
answers `has_logs` and reading one deployment is what hands the output
over. That is also the shape the dashboard wants: a row you open, which
then polls itself while the deploy is still running.

`Image.Local` is what stops the orchestrator pulling something it just
built — a registry that has never heard of that image would be the only
place to look.

## Who may build

`Source.Builds()` is the question authorization asks, and `RoleToDeploy`
is the answer: running an image someone already published is a
**member's** job, turning source into an image is an **admin's**. A build
executes whatever the source contains, on this host, with the builder's
privileges — a different kind of act from running a published artifact.

It binds creating an app, deploying one, **and writing its environment**.
A member who could create an app they can never deploy would be an odd
thing to allow — and one who could write a building app's environment
would be building through it. For an app that builds, the environment is
build input as well as the container's: Railpack reads it to work out how
to build the repository and turns `RAILPACK_INSTALL_CMD`,
`RAILPACK_BUILD_CMD` and `RAILPACK_START_CMD` into commands the build
runs. The app's own variables win the merge over its environment's and
its project's, both of which are already an admin's, so the app level was
the way round them. Reading stays a member's: seeing how an app is
configured is not deciding what it builds.

Deploy resolves as a member first and checks the source's own
requirement after, so a member deploying a building app is told they lack
the role rather than that the app is missing.

Every source is listed in `TestTheRoleEachSourceNeeds`, so adding one is
a decision about its role rather than an accident.
`TestOnlyAnAdminMayCreateOrDeployABuildingApp` and
`TestOnlyAnAdminMayWriteABuildingAppsEnv` pin the two ways in.

## Deploys are detached

`Orchestrator.Start` records a `deployments` row, returns it, and does
the work on a goroutine with a context of its own. Nothing that asks for
a deploy is holding it up: a client that times out, hangs up, or presses
Ctrl-C stops waiting, not deploying. Both entry points — `POST
.../deploy` (202) and the registry webhook — go through it.

That goroutine recovers. An unrecovered panic there would take the daemon
down and every app it proxies with it, which is far worse than one failed
deploy; the panic becomes the deployment's error instead.

How a deploy went lives in its row, since nobody is on the connection to
be told. `WaitFor` polls it, `?wait=true` does the same over HTTP, and
abandoning either does not touch the deploy.

## Deleting

**Deleting something takes everything under it.** A project takes its
environments and apps; an environment takes its apps. Every app's container is stopped and
removed on the way.

It used to refuse at each level instead, and that was bookkeeping rather
than a safeguard: reaching a project you wanted gone meant deleting its
apps one at a time first, and the daemon would have carried out every one
of those deletes anyway. The safeguard is the confirmation in front of it
— `ConfirmDialog` asks for the thing's own name, and the CLI wants
`--yes` — not a refusal you can satisfy by hand.

`production` is the one refusal left. It is created with its project and
every app assumes it exists, so it goes when the project does and never
before.

The order is containers first, then rows, and outside the transaction —
Docker has no rollback. A failure there leaves the apps gone and the
thing above them still standing, which a retry finishes; the reverse
would leave a container running with nothing on the instance naming it.
`project.AppTeardown` is the seam: `project` sits below `app` and cannot
import it, so `server` hands the app service back up at wiring time, and
a service with no teardown wired refuses to delete at all.

Deleting an app leaves its images in the registry. Reclaiming that disk
is a registry garbage collection pass, and `internal/registry` has one —
`registry garbage-collect --delete-untagged` inside the registry's own
container, which is what that module's `Maintainer` exists to run.
**Nothing schedules it**: it is a button, not a timer, because the pass
wants the registry stopped and that is a few seconds of every app's
pushes failing.

**And it is the other half of every delete beside it.** A `registry:2`
delete unlinks a manifest and leaves the layers, so a screen that offers
deleting without offering this is one where somebody clears a repository
and watches the disk not move. The two live together on Cubeship's own
registry page, which until recently offered neither: the endpoints were
there from the start and the dashboard hid them, so the one registry
this instance runs was the one you could not tidy up.

**Deleting a deployment deletes a record — except the live one, which
takes the app down.** That is the one place in this product where a
delete means two things, and it is deliberate: an app whose running
version has to go *now* — a compromised image, something doing what it
should not — must not force somebody to delete the app and lose its
domains, its environment and everything it is attached to. So the
container is stopped and removed, the app goes to `down`, and the app
itself stays, coming back on the next deploy. Anything else is history:
the container it produced is long gone, and what goes with the row is
the build log.

`live` on the listing is what says which. It is derived rather than
stored, from two facts that are already there: the app has a container,
and the orchestrator swaps a container in and *then* marks the
deployment succeeded — so the newest succeeded row is by construction
the one behind what is running. An app with no container is running no
deployment whatever its history says, which is why nothing inherits the
title when the live one is deleted.

One row is refused, and `deletable` says so in advance: a deploy that
has **not finished**, because the orchestrator is still writing to it.

The role is the one that deploys that app — `RoleToDeploy` — because
somebody who may replace what is running may take it off. What stands
between a tidy-up and an outage is not a role but the confirmation:
clearing an old row asks for a second click, and the live one asks you
to type the app's name.
