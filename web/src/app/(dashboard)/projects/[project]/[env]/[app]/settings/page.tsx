"use client";

import { ChevronLeftIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AppNetwork } from "@/components/app-network";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { GitHubSource } from "@/components/github-source";
import { CUBESHIP, ImageSource, type ImageSourceValue } from "@/components/image-source";
import { LoadingList } from "@/components/loading";
import { Notice } from "@/components/notice";
import { OptionCards } from "@/components/option-cards";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { TextAreaField, TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  type App,
  type AppAutoscale,
  type AppLimits,
  type AppReplica,
  type AppSource,
  api,
  BUILDING_SOURCES,
  type ClusterServer,
  // Aliased: this file's own Settings is the screen.
  type Settings as InstanceSettings,
} from "@/lib/api";
import { message } from "@/lib/errors";

// The daemon has four sources. There are only two things an app can be:
// something this instance builds, or something someone else already
// built. Which of the two ways it is built — and which of the two
// registries an image comes from — is the next question down.
type Origin = "github" | "image";

const SOURCE: Record<string, AppSource> = { railpack: "railpack", dockerfile: "dockerfile" };

export default function AppSettingsPage({
  params,
}: PageProps<"/projects/[project]/[env]/[app]/settings">) {
  const { project, env, app } = use(params);
  return <Settings reference={`${project}/${env}/${app}`} />;
}

function Settings({ reference }: { reference: string }) {
  const router = useRouter();
  const [app, setApp] = useState<App | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const path = `/apps/${reference}`;
  const reload = useCallback(() => {
    if (!reference) return;
    api
      .get<App>(path)
      .then(setApp)
      .catch((e) => setError(message(e)));
  }, [path, reference]);
  useEffect(reload, [reload]);

  if (!reference) {
    return (
      <p className="text-sm text-muted-foreground">
        No app named.{" "}
        <Link href="/projects" className="text-foreground underline underline-offset-4">
          Back to projects
        </Link>
        .
      </p>
    );
  }
  if (error && !app) return <ErrorAlert error={error} />;

  return (
    <>
      <Link
        href={`/projects/${reference}`}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {reference}
      </Link>

      <PageHeader title="App settings" />

      <ErrorAlert error={error} />

      {/* The heading above says the same thing for every app and comes
          from the URL, so it does not wait — see the app's own page for
          why returning null until the answer lands made every
          navigation blink. */}
      {!app && <LoadingList rows={4} />}

      {app && (
        <>
          <General app={app} onSaved={setApp} onError={setError} />
          <AppNetwork app={app} onSaved={setApp} />
          <SourceSection app={app} onSaved={setApp} onError={setError} />
          <Placement app={app} onSaved={setApp} onError={setError} />
          <Limits app={app} onSaved={setApp} onError={setError} />
          <AutoscaleSection app={app} onSaved={setApp} onError={setError} />

          <DangerZone>
            <DangerAction
              title="Delete this app"
              description="Its container is stopped first. Images already pushed stay in the registry — reclaiming that disk needs a garbage collection pass Cubeship does not run."
              action={
                <Button variant="destructive" onClick={() => setDeleting(true)}>
                  Delete app
                </Button>
              }
            />
          </DangerZone>

          <ConfirmDialog
            open={deleting}
            onOpenChange={setDeleting}
            title="Delete app"
            description="The container serving it is stopped and the app is gone. This cannot be undone."
            confirmWord={app.name}
            confirmLabel="Delete app"
            onConfirm={async () => {
              await api.del(path);
              router.push(`/projects/${app.project}/${app.environment}`);
            }}
          />
        </>
      )}
    </>
  );
}

type SectionProps = {
  app: App;
  onSaved: (a: App) => void;
  onError: (m: string | null) => void;
};

// patch is every section's save: PATCH leaves out what it does not
// mention, so one section cannot blank another's fields.
function usePatch({ app, onSaved, onError }: SectionProps) {
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);

  // A patch body is one field group's worth of an app. Placement sends
  // a list of machines and a count, which is why this is not a map of
  // strings.
  async function save(
    body: Record<string, string | string[] | number | boolean | AppLimits | AppAutoscale>,
  ) {
    setBusy(true);
    onError(null);
    setSaved(false);
    try {
      onSaved(await api.patch<App>(`/apps/${app.reference}`, body));
      setSaved(true);
    } catch (err) {
      onError(message(err));
    }
    setBusy(false);
  }

  return { busy, saved, setSaved, save };
}

function General(props: SectionProps) {
  const { app } = props;
  const { busy, saved, setSaved, save } = usePatch(props);
  const [description, setDescription] = useState(app.description ?? "");
  const dirty = description !== (app.description ?? "");

  return (
    <>
      <SectionHeader title="General" />
      <Card>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              save({ description });
            }}
          >
            <TextAreaField
              label="Description"
              hint="What this app is. Empty is fine."
              rows={3}
              value={description}
              onChange={(e) => {
                setDescription(e.target.value);
                setSaved(false);
              }}
            />

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">Reference</Label>
              <div className="flex h-10 items-center border border-border bg-secondary/40 px-3 font-mono text-sm text-muted-foreground">
                {app.reference}
              </div>
              <p className="text-xs text-subtle-foreground">
                Not editable. It is this app&apos;s registry repository path and the basis of its
                container and router names.
              </p>
            </div>

            <SaveRow busy={busy} saved={saved} dirty={dirty} />
          </form>
        </CardContent>
      </Card>
    </>
  );
}

// Which machines in the cluster run the app, and which of them its
// traffic arrives at.
//
// The section is only here when there is a choice to make: on an
// instance of one box there is one machine, and a set with one possible
// member is a decision nobody has.
// AutoscaleSection hands the replica count to the instance.
//
// Below Limits and below Servers, because it is the decision that only
// makes sense once you have made those: how much one copy may take, and
// where copies may go.
function AutoscaleSection(props: SectionProps) {
  const { app } = props;
  const { busy, saved, setSaved, save } = usePatch(props);
  const [on, setOn] = useState(app.autoscale.max > 0);
  const [minReplicas, setMin] = useState(String(app.autoscale.min || 1));
  const [maxReplicas, setMax] = useState(String(app.autoscale.max || ""));
  const [cpu, setCPU] = useState(String(app.autoscale.cpu || 70));

  const next: AppAutoscale = on
    ? { min: Number(minReplicas) || 0, max: Number(maxReplicas) || 0, cpu: Number(cpu) || 0 }
    : { min: app.autoscale.min, max: 0, cpu: app.autoscale.cpu };
  const dirty =
    next.max !== app.autoscale.max ||
    (on && (next.min !== app.autoscale.min || next.cpu !== app.autoscale.cpu));

  return (
    <>
      <SectionHeader
        title="Autoscaling"
        sub="Let this instance decide how many copies to run, from the average CPU across them over the last three minutes — the same reading the chart on this app's page shows."
      />
      <Card>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              save({ autoscale: next });
            }}
          >
            {/* A switch, not a checkbox: it turns a behaviour on. */}
            <div className="flex items-start gap-3">
              <Switch
                id="autoscale"
                checked={on}
                onCheckedChange={(v) => {
                  setOn(v === true);
                  setSaved(false);
                }}
                className="mt-0.5"
              />
              <label htmlFor="autoscale" className="text-xs leading-relaxed text-muted-foreground">
                Decide the count for me. Turning it off leaves the app at whatever it is running;
                nothing is stopped.
              </label>
            </div>

            {on && (
              <>
                <div className="grid gap-4 sm:grid-cols-2">
                  <TextField
                    label="Fewest copies"
                    type="number"
                    min="1"
                    value={minReplicas}
                    onChange={(e) => {
                      setMin(e.target.value);
                      setSaved(false);
                    }}
                  />
                  <TextField
                    label="Most copies"
                    type="number"
                    min="1"
                    value={maxReplicas}
                    onChange={(e) => {
                      setMax(e.target.value);
                      setSaved(false);
                    }}
                  />
                </div>
                <p className="text-xs leading-relaxed text-muted-foreground">
                  A ceiling is not a formality. Without one a loop of requests is a loop of replicas
                  until the machine has nothing left, which is a worse outage than the one this is
                  on to avoid.
                </p>

                <TextField
                  label="Target CPU per copy"
                  type="number"
                  step="1"
                  min="1"
                  value={cpu}
                  onChange={(e) => {
                    setCPU(e.target.value);
                    setSaved(false);
                  }}
                  hint="100 is one core, the same scale this app's chart is drawn on — so the number here is the number you were looking at. CPU is the only signal: adding a copy does not lower any copy's memory, so a memory rule would climb and never come back."
                />

                <Notice>
                  It is damped, and none of that is adjustable: within 10% of target nothing moves,
                  and after a change it waits three minutes before the next — ten before a smaller
                  one, because an extra copy costs some memory and one copy too few costs this app
                  its latency exactly as load comes back.
                  {app.autoscale.at &&
                    ` Last changed ${new Date(app.autoscale.at).toLocaleString()}.`}
                </Notice>
              </>
            )}
            <SaveRow busy={busy} saved={saved} dirty={dirty} />
          </form>
        </CardContent>
      </Card>
    </>
  );
}

// Limits caps what one copy of the app may take from the machine it
// runs on.
//
// Its own section rather than a field in Placement, and not hidden
// behind having a cluster: an app on one box is exactly the app most
// worth capping, because the box it can take down is the one running
// everything else.
function Limits(props: SectionProps) {
  const { app } = props;
  const { busy, saved, setSaved, save } = usePatch(props);
  const [cpu, setCPU] = useState(app.limits.cpu ? String(app.limits.cpu) : "");
  const [memory, setMemory] = useState(
    app.limits.memory_bytes ? String(Math.round(app.limits.memory_bytes / (1 << 20))) : "",
  );

  const next = { cpu: Number(cpu) || 0, memory_bytes: (Number(memory) || 0) * (1 << 20) };
  const dirty = next.cpu !== app.limits.cpu || next.memory_bytes !== app.limits.memory_bytes;

  return (
    <>
      <SectionHeader
        title="Limits"
        sub="How much of its machine one copy of this app may take. Empty is no limit, which is what every app starts as. Changing one takes effect within seconds and does not restart anything; removing one waits for the next deploy."
      />
      <Card>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              save({ limits: next });
            }}
          >
            <TextField
              label="CPU"
              type="number"
              step="0.01"
              min="0"
              placeholder="no limit"
              value={cpu}
              onChange={(e) => {
                setCPU(e.target.value);
                setSaved(false);
              }}
              hint={`Cores, and fractional is normal here: 0.5 is half a core. It is a ceiling rather than a share — a container at its limit is throttled, not merely preferred less when the machine is busy.${
                app.scale > 1
                  ? ` Per copy: this app runs ${app.scale} of itself, so they may take ${app.scale} times this between them.`
                  : ""
              }`}
            />

            <TextField
              label="Memory (MiB)"
              type="number"
              min="0"
              placeholder="no limit"
              value={memory}
              onChange={(e) => {
                setMemory(e.target.value);
                setSaved(false);
              }}
              hint="A hard ceiling. The kernel enforces it by killing whatever crosses it, so setting one below what the app is already using stops it there and then."
            />

            {!dirty && app.limits.cpu === 0 && app.limits.memory_bytes === 0 && (
              <Notice>
                Nothing caps this app. One copy of it can take the whole machine — including from
                the daemon, the proxy and everything else on the box.
              </Notice>
            )}
            <SaveRow busy={busy} saved={saved} dirty={dirty} />
          </form>
        </CardContent>
      </Card>
    </>
  );
}

function Placement(props: SectionProps) {
  const { app } = props;
  const { busy, saved, setSaved, save } = usePatch(props);
  const [servers, setServers] = useState<ClusterServer[] | null>(null);
  const [registryHost, setRegistryHost] = useState<string | null>(null);
  const [nodes, setNodes] = useState<string[]>(app.nodes);
  const [scale, setScale] = useState(app.scale);
  const [spread, setSpread] = useState(app.spread === true);

  useEffect(() => {
    api
      .get<ClusterServer[]>("/nodes")
      .then(setServers)
      .catch(() => setServers([]));
    // Whether a build has anywhere to go. The registry follows the
    // instance's domain, so an instance with no domain has none — and
    // an app that builds cannot leave this machine until it does.
    api
      .get<InstanceSettings>("/settings")
      .then((s) => setRegistryHost(s.registry_host ?? ""))
      .catch(() => setRegistryHost(""));
  }, []);

  if (servers !== null && servers.length < 2) return null;

  const dirty =
    !sameSet(nodes, app.nodes) || scale !== app.scale || spread !== (app.spread === true);
  // The one thing left that keeps an app here, said before the request
  // rather than after it. A name no longer does: every name arrives at
  // this instance whichever machine runs the app, so moving one is not
  // a DNS change. What an app that builds needs is
  // somewhere to push to, and that is the instance's own registry,
  // which exists once there is a domain.
  const stuck =
    BUILDING_SOURCES.includes(app.source) && registryHost === ""
      ? "It is built here, and a build reaches another machine only through this instance's own registry — which needs a domain."
      : null;

  const toggle = (name: string, on: boolean) => {
    const next = on ? [...nodes, name] : nodes.filter((n) => n !== name);
    setNodes(next);
    // Never fewer copies than machines: the daemon refuses it, and a
    // form that can hold a state the request is rejected for is a form
    // that teaches people to distrust the button.
    if (scale < next.length) setScale(next.length);
    setSaved(false);
  };

  return (
    <>
      <SectionHeader
        title="Servers"
        sub="Which machines in this cluster run it, and how many copies. More than one puts this instance's proxy in front of every copy, over the cluster's private network. Where its traffic arrives never changes — every name arrives here. Changes take effect within a few seconds: each new machine starts the app before the ones leaving stop it."
      />
      <Card>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              // Following the cluster is the answer to "which
              // machines", so the tick list is not sent alongside it:
              // sending both would turn the switch straight back off.
              save(spread ? { spread: true, scale } : { nodes, scale, spread: false });
            }}
          >
            {/* A switch, not a checkbox: the ticks below pick machines
                out of a list, and this turns a behaviour on. */}
            <div className="flex items-start gap-3">
              <Switch
                id="spread"
                checked={spread}
                onCheckedChange={(next) => {
                  setSpread(next === true);
                  setSaved(false);
                }}
                className="mt-0.5"
              />
              <label htmlFor="spread" className="text-xs leading-relaxed text-muted-foreground">
                Follow the cluster. It runs on every machine there is, and on any that joins later —
                so a new server takes a share without this app being edited. Ticking machines below
                turns it off again, because that is choosing by hand.
              </label>
            </div>

            <div className={spread ? "space-y-2 opacity-50" : "space-y-2"}>
              <Label>Runs on</Label>
              {(servers ?? []).map((s) => {
                const on = nodes.includes(s.name);
                const here = app.replicas.filter((r) => r.node === s.name);
                const replica = here[0];
                return (
                  <label
                    key={s.name}
                    htmlFor={`runs-on-${s.name}`}
                    className="flex items-center gap-3 border border-border px-3 py-2 font-mono text-xs"
                  >
                    <Checkbox
                      id={`runs-on-${s.name}`}
                      checked={on}
                      disabled={spread || stuck !== null || (on && nodes.length === 1)}
                      onCheckedChange={(next) => toggle(s.name, next === true)}
                    />
                    <span className="flex-1">{s.name}</span>
                    <span className="text-muted-foreground">
                      {here.length > 1 && `${here.length} copies · `}
                      {replica ? replicaState(replica) : s.control_plane ? "this machine" : ""}
                    </span>
                  </label>
                );
              })}
              <p className="text-xs text-muted-foreground">
                {spread
                  ? "Decided by the switch above: every machine in the cluster, and any that joins."
                  : "An app has to run somewhere, so the last machine cannot be unticked. Take it off one by putting it on another first."}
              </p>
            </div>

            <TextField
              label="Copies"
              type="number"
              min={String(spread ? (servers?.length ?? 1) : nodes.length)}
              value={String(scale)}
              onChange={(e) => {
                setScale(Number(e.target.value));
                setSaved(false);
              }}
              hint={`How many run in total, spread over those machines round-robin: four over three is 2, 1, 1. Never fewer than the ${nodes.length} machine${nodes.length === 1 ? "" : "s"} ticked above — one given nothing to run is one placed there for no effect. Several on a machine are swapped one at a time, so a deploy rolls rather than leaving none of them serving.`}
            />

            {stuck && (
              <Notice>
                {stuck} It stays on this machine until one is set in the instance settings.
              </Notice>
            )}
            {!stuck && !dirty && app.nodes.length > 1 && (
              <Notice>
                Its traffic arrives at this instance and is spread across {app.nodes.length}{" "}
                machines from there, over the cluster&apos;s private network. Each copy keeps its
                own log and its own charts; the app&apos;s are the average across them.
              </Notice>
            )}
            {!stuck && !dirty && app.nodes.length === 1 && app.nodes[0] !== "control-plane" && (
              <Notice>
                Its traffic still arrives at this instance and is routed there over the cluster's
                private network. Its charts and its log come from that machine through the
                connection it keeps open to this one.
              </Notice>
            )}
            <SaveRow busy={busy} saved={saved} dirty={dirty} />
          </form>
        </CardContent>
      </Card>
    </>
  );
}

// replicaState says what one machine's copy is doing, in the two words
// that matter: whether it is up, and whether the edge is actually
// sending it anything. The second is not the first — a replica can be
// running and not yet be a backend.
function replicaState(r: AppReplica): string {
  if (r.status !== "running") return r.status;
  return r.serving ? "serving" : "running, not yet a backend";
}

function sameSet(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((x) => b.includes(x));
}

function SourceSection(props: SectionProps) {
  const { app } = props;
  const { busy, saved, setSaved, save } = usePatch(props);

  const builds = BUILDING_SOURCES.includes(app.source);
  const [origin, setOrigin] = useState<Origin>(builds ? "github" : "image");
  const [buildWith, setBuildWith] = useState(
    app.source === "dockerfile" ? "dockerfile" : "railpack",
  );
  const [repo, setRepo] = useState(app.repo ?? "");
  const [gitRef, setGitRef] = useState(app.ref ?? "");
  const [dockerfile, setDockerfile] = useState(app.dockerfile ?? "");
  // Which registry, which image and which tag are one value: changing
  // the registry invalidates the other two, and a form that held them
  // apart would let them disagree between one render and the next.
  const [from, setFrom] = useState<ImageSourceValue>({
    registry: app.source === "external" ? hostOf(app.image ?? "") : CUBESHIP,
    image: app.source === "external" ? (app.image ?? "") : "",
    tag: app.tag ?? "",
  });

  // A registry that is not this instance's own is the external source,
  // whichever one it is. The dashboard shows the registry; the daemon
  // has always cared only whether it runs it.
  const source: AppSource =
    origin === "github" ? SOURCE[buildWith] : from.registry === CUBESHIP ? "registry" : "external";
  const nowBuilds = origin === "github";
  const problem = originProblem(source, { repo, image: from.image });

  const touch = () => setSaved(false);

  return (
    <>
      <SectionHeader title="Source" sub="Where this app's image comes from." />
      <Card>
        <CardContent>
          <form
            className="space-y-5"
            onSubmit={(e) => {
              e.preventDefault();
              // The source and its settings travel together: the daemon
              // judges them as one, and refuses a setting the source
              // would ignore.
              save({
                source,
                image: source === "external" ? from.image.trim() : "",
                tag: nowBuilds ? "" : from.tag.trim(),
                repo: nowBuilds ? repo.trim() : "",
                ref: nowBuilds ? gitRef.trim() : "",
                dockerfile: source === "dockerfile" ? dockerfile.trim() : "",
              });
            }}
          >
            <OptionCards<Origin>
              value={origin}
              onChange={(v) => {
                setOrigin(v);
                touch();
              }}
              options={[
                {
                  value: "github",
                  title: "Git provider",
                  body: "Cubeship clones the repository and builds it here, so what runs is code this instance compiled.",
                },
                {
                  value: "image",
                  title: "Docker image",
                  body: "An image someone already built and published. Cubeship runs it as it is.",
                },
              ]}
            />

            {origin === "github" ? (
              <div className="space-y-5 border-l-2 border-primary/40 pl-4">
                <GitHubSource
                  repo={repo}
                  gitRef={gitRef}
                  onRepo={(url, defaultBranch) => {
                    setRepo(url);
                    // A repository's default branch is the right answer
                    // until someone says otherwise, and choosing a new
                    // repository makes the old branch meaningless.
                    setGitRef(defaultBranch);
                    touch();
                  }}
                  onRef={(v) => {
                    setGitRef(v);
                    touch();
                  }}
                />

                <OptionCards
                  label="How it is built"
                  value={buildWith}
                  onChange={(v) => {
                    setBuildWith(v);
                    touch();
                  }}
                  options={[
                    {
                      value: "railpack",
                      title: "Railpack",
                      body: "No Dockerfile needed — Railpack reads the repository, works out what it is, and produces the build.",
                    },
                    {
                      value: "dockerfile",
                      title: "Dockerfile",
                      body: "Build the Dockerfile in the repository, exactly as written.",
                    },
                  ]}
                />

                {buildWith === "dockerfile" && (
                  <TextField
                    label="Dockerfile path"
                    hint="Optional. Relative to the repository root."
                    spellCheck={false}
                    value={dockerfile}
                    onChange={(e) => {
                      setDockerfile(e.target.value);
                      touch();
                    }}
                    placeholder="Dockerfile"
                  />
                )}
              </div>
            ) : (
              <div className="space-y-5 border-l-2 border-primary/40 pl-4">
                <ImageSource
                  value={from}
                  pushPath={app.source === "registry" ? app.image : undefined}
                  appReference={app.reference}
                  onChange={(next) => {
                    setFrom(next);
                    touch();
                  }}
                />
              </div>
            )}

            {problem && <ErrorAlert error={problem} />}
            <SaveRow busy={busy} saved={saved} dirty={!problem} />
          </form>
        </CardContent>
      </Card>
    </>
  );
}

function SaveRow({ busy, saved, dirty }: { busy: boolean; saved: boolean; dirty: boolean }) {
  return (
    <div className="flex items-center gap-3">
      <ActionButton type="submit" busy={busy} disabled={!dirty}>
        Save
      </ActionButton>
      {saved && <span className="text-xs text-muted-foreground">Saved.</span>}
    </div>
  );
}

// The same refusals the daemon makes, said here instead — so a mistake
// is a sentence under the field rather than a rejected submit. The
// daemon still checks: this is a courtesy, not the rule.
function originProblem(source: AppSource, o: { repo: string; image: string }): string | null {
  if (source === "external") {
    const image = o.image.trim();
    if (!image) return null;
    if (image.includes("://") || /\s/.test(image)) {
      return "That is an image reference, not a URL — registry.example.com/acme/api.";
    }
    if ((image.split("/").pop() ?? "").includes(":")) {
      return "Leave the tag off the image. The tag has a field of its own, and two places to say it is one that can contradict the other.";
    }
    return null;
  }

  if (source === "dockerfile" || source === "railpack") {
    const repo = o.repo.trim();
    if (!repo) return null;
    if (!/^(https|http|git):\/\//.test(repo)) {
      return "The repository must be an https://, http:// or git:// URL — ssh needs a key this instance does not have.";
    }
    if (repo.includes("#")) {
      return "Put the branch or commit in its own field, not in the URL.";
    }
  }
  return null;
}

// hostOf is which registry an image reference names, in the spelling the
// daemon uses: a reference with no registry in it at all is Docker Hub's.
function hostOf(image: string): string {
  const first = image.split("/")[0] ?? "";
  if (!image.includes("/") || (!/[.:]/.test(first) && first !== "localhost")) {
    return "index.docker.io";
  }
  return first.toLowerCase();
}
