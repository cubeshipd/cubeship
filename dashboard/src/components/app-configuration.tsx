"use client";

import { XIcon } from "lucide-react";
import { useCallback, useEffect, useId, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AppNetwork } from "@/components/app-network";
import { AppTCPPorts } from "@/components/app-tcp-ports";
import { AppVolumes } from "@/components/app-volumes";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { GitHubSource } from "@/components/github-source";
import { GlideSelect } from "@/components/glide-select";
import { CUBESHIP, ImageSource, type ImageSourceValue } from "@/components/image-source";
import { Notice } from "@/components/notice";
import { ResourceLimitFields } from "@/components/resource-limit-fields";
import { SectionHeader } from "@/components/section-header";
import { TextField } from "@/components/text-field";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  type App,
  type AppAutoscale,
  type AppLimits,
  type AppReplica,
  type AppSource,
  type AppTCPPort,
  type AppVolume,
  api,
  BUILDING_SOURCES,
  type ClusterServer,
  type Settings as InstanceSettings,
} from "@/lib/api";
import { message } from "@/lib/errors";

// The daemon has four sources. There are only two things an app can be:
// something this instance builds, or something someone else already
// built. Which of the two ways it is built — and which of the two
// registries an image comes from — is the next question down.
type Origin = "github" | "image";

const SOURCE: Record<string, AppSource> = { railpack: "railpack", dockerfile: "dockerfile" };

export type AppConfigurationSection = "build" | "network" | "resources";

export function AppConfiguration({
  reference,
  app,
  section,
  deleteOpen,
  onDeleteOpenChange,
  onAppChange,
  onDeleted,
}: {
  reference: string;
  app: App;
  section: AppConfigurationSection;
  deleteOpen: boolean;
  onDeleteOpenChange: (open: boolean) => void;
  onAppChange: (app: App) => void;
  onDeleted: () => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const [deleteVolumeData, setDeleteVolumeData] = useState(false);
  const [volumes, setVolumes] = useState<AppVolume[] | null>(null);
  const [ports, setPorts] = useState<AppTCPPort[] | null>(null);

  const path = `/apps/${reference}`;
  const reloadVolumes = useCallback(() => {
    api
      .get<AppVolume[]>(`${path}/volumes`)
      .then(setVolumes)
      .catch(() => setVolumes([]));
  }, [path]);
  useEffect(reloadVolumes, [reloadVolumes]);
  const reloadPorts = useCallback(() => {
    api
      .get<AppTCPPort[]>(`${path}/tcp-ports`)
      .then(setPorts)
      .catch(() => setPorts([]));
  }, [path]);
  useEffect(reloadPorts, [reloadPorts]);
  const pinned = (volumes?.length ?? 0) > 0;
  const published = (ports?.length ?? 0) > 0;

  return (
    <>
      <ErrorAlert error={error} />
      {section === "network" && (
        <div className="app-network-settings space-y-8">
          <AppNetwork app={app} onSaved={onAppChange} />
          <AppTCPPorts app={app} ports={ports} onChanged={reloadPorts} />
        </div>
      )}
      {section === "build" && (
        <div className="space-y-6">
          <SourceSection app={app} onSaved={onAppChange} onError={setError} />
          <PredeploySection app={app} onSaved={onAppChange} onError={setError} />
          <StopPolicySection app={app} onSaved={onAppChange} onError={setError} />
        </div>
      )}
      {section === "resources" && (
        <div className="space-y-6">
          {pinned || published ? (
            <Card>
              <CardContent className="space-y-4">
                <SectionHeader title="Placement" />
                <Notice>
                  Pinned to {volumes?.[0]?.node ?? app.nodes[0]} by{" "}
                  {[pinned && "persistent volumes", published && "published TCP ports"]
                    .filter(Boolean)
                    .join(" and ")}
                  . Remove them to change placement or scale the app.
                </Notice>
              </CardContent>
            </Card>
          ) : (
            <Placement app={app} onSaved={onAppChange} onError={setError} />
          )}
          <Limits app={app} onSaved={onAppChange} onError={setError} />
          {!pinned && !published && (
            <AutoscaleSection app={app} onSaved={onAppChange} onError={setError} />
          )}
          <AppVolumes app={app} volumes={volumes} onChanged={reloadVolumes} />
        </div>
      )}

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={onDeleteOpenChange}
        title="Delete app"
        description="The container serving it is stopped and the app is gone. This cannot be undone."
        holdToConfirm
        confirmLabel="Delete app"
        onConfirm={async () => {
          await api.del(deleteVolumeData ? `${path}?delete_volume_data=true` : path);
          onDeleted();
        }}
      >
        {pinned && (
          <>
            <Label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={!deleteVolumeData}
                onCheckedChange={(v) => setDeleteVolumeData(!v)}
              />
              Keep its volumes&apos; data
            </Label>
            {deleteVolumeData && (
              <Notice tone="warning">
                The data in {volumes?.map((v) => v.path).join(", ")} is deleted permanently.
              </Notice>
            )}
          </>
        )}
      </ConfirmDialog>
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
    body: Record<
      string,
      string | string[] | number | boolean | AppLimits | AppAutoscale | Record<string, string>
    >,
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

// Which machines in the cluster run the app, and which of them its
// traffic arrives at.
//
// The section is only here when there is a choice to make: on an
// instance of one box there is one machine, and a set with one possible
// member is a decision nobody has.
function PredeploySection(props: SectionProps) {
  const { app } = props;
  const { busy, saved, save } = usePatch(props);
  const initial = typeof app.predeploy_command === "string" ? app.predeploy_command : "";
  const [command, setCommand] = useState(initial);
  const [timeout, setTimeout] = useState(String(app.predeploy_timeout || 600));
  const dirty = command !== initial || Number(timeout) !== (app.predeploy_timeout || 600);
  return (
    <Card>
      <CardContent className="space-y-4">
        <SectionHeader title="Pre-deploy command" />
        <p className="text-sm text-muted-foreground">
          Runs from the new image with the app environment and network before the old container is
          replaced. A non-zero exit or timeout keeps the current version serving.
        </p>
        <Textarea
          className="min-h-24 rounded-lg px-3 py-2 font-mono text-sm"
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          placeholder="./bin/migrate"
          aria-label="Pre-deploy shell command"
        />
        <TextField
          label="Timeout (seconds, maximum 3600)"
          value={timeout}
          onChange={(e) => setTimeout(e.target.value)}
          type="number"
        />
        <div className="flex items-center gap-3">
          <Button
            disabled={!dirty || busy}
            onClick={() =>
              save({ predeploy_command: command, predeploy_timeout: Number(timeout) || 0 })
            }
          >
            {busy ? "Saving…" : "Save"}
          </Button>
          {saved && <span className="text-sm text-muted-foreground">Saved</span>}
        </div>
      </CardContent>
    </Card>
  );
}

function StopPolicySection(props: SectionProps) {
  const { app } = props;
  const { busy, saved, save } = usePatch(props);
  const [grace, setGrace] = useState(String(app.stop_grace_period ?? 10));
  const [signal, setSignal] = useState(app.stop_signal || "SIGTERM");
  const dirty =
    Number(grace) !== (app.stop_grace_period ?? 10) || signal !== (app.stop_signal || "SIGTERM");
  const valid =
    /^SIG[A-Z0-9_]+$/.test(signal) &&
    signal !== "SIGKILL" &&
    signal !== "SIGSTOP" &&
    Number(grace) >= 0 &&
    Number(grace) <= 3600;
  return (
    <Card className="mt-4">
      <CardContent className="space-y-4">
        <SectionHeader
          title="Container stop policy"
          sub="How long a container gets to shut down cleanly before it is force-stopped."
        />
        <TextField
          label="Grace period (seconds, 0–3600)"
          type="number"
          value={grace}
          onChange={(e) => setGrace(e.target.value)}
        />
        <TextField
          label="Stop signal"
          hint="Defaults to SIGTERM. SIGKILL and SIGSTOP are not allowed."
          value={signal}
          onChange={(e) => setSignal(e.target.value.toUpperCase())}
        />
        {!valid && <ErrorAlert error="Enter a valid grace period and a non-forcing SIG signal." />}
        <div className="flex items-center gap-3">
          <Button
            disabled={!dirty || !valid || busy}
            onClick={() => save({ stop_grace_period: Number(grace), stop_signal: signal })}
          >
            {busy ? "Saving…" : "Save"}
          </Button>
          {saved && <span className="text-sm text-muted-foreground">Saved</span>}
        </div>
      </CardContent>
    </Card>
  );
}

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
    <Card>
      <CardContent className="space-y-4">
        <SectionHeader
          title="Autoscaling"
          sub="Scale copies to keep average CPU near your target."
        />
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
                Set a maximum to prevent runaway scaling.
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
                hint="100% is one core. Autoscaling uses CPU, not memory."
              />

              <Notice>
                Uses the three-minute average CPU. Scale-down waits ten minutes after a change.
                {app.autoscale.at &&
                  ` Last changed ${new Date(app.autoscale.at).toLocaleString()}.`}
              </Notice>
            </>
          )}
          <SaveRow busy={busy} saved={saved} dirty={dirty} />
        </form>
      </CardContent>
    </Card>
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
    <Card>
      <CardContent className="space-y-4">
        <SectionHeader
          title="Limits"
          sub="Set optional CPU and memory ceilings for each replica."
        />
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            save({ limits: next });
          }}
        >
          <ResourceLimitFields
            cpu={cpu}
            memory={memory}
            onCPUChange={(value) => {
              setCPU(value);
              setSaved(false);
            }}
            onMemoryChange={(value) => {
              setMemory(value);
              setSaved(false);
            }}
          />

          {!dirty && app.limits.cpu === 0 && app.limits.memory_bytes === 0 && (
            <Notice>No limits set. A copy can use all available CPU and memory.</Notice>
          )}
          <SaveRow busy={busy} saved={saved} dirty={dirty} />
        </form>
      </CardContent>
    </Card>
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
    <Card>
      <CardContent className="space-y-4">
        <SectionHeader
          title="Placement"
          sub="Choose which machines run this app and how many copies."
        />
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
            <label htmlFor="spread" className="text-sm leading-relaxed text-muted-foreground">
              Run on every machine in the cluster.
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
                ? "Managed by the cluster-wide setting above."
                : "Keep at least one machine selected."}
            </p>
          </div>

          <TextField
            label="Replicas"
            type="number"
            min={String(spread ? (servers?.length ?? 1) : nodes.length)}
            value={String(scale)}
            onChange={(e) => {
              setScale(Number(e.target.value));
              setSaved(false);
            }}
            hint={`Replicas are spread across the selected machines. Minimum: ${spread ? (servers?.length ?? 1) : nodes.length}.`}
          />

          {stuck && (
            <Notice>
              {stuck} It stays on this machine until one is set in the instance settings.
            </Notice>
          )}
          {!stuck && !dirty && app.nodes.length > 1 && (
            <Notice>
              Traffic is routed through this instance to all {app.nodes.length} machines.
            </Notice>
          )}
          {!stuck && !dirty && app.nodes.length === 1 && app.nodes[0] !== "control-plane" && (
            <Notice>Traffic is routed through this instance to the selected machine.</Notice>
          )}
          <SaveRow busy={busy} saved={saved} dirty={dirty} />
        </form>
      </CardContent>
    </Card>
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
  const [watchPaths, setWatchPaths] = useState(app.watch_paths ?? []);
  const [ignorePaths, setIgnorePaths] = useState(app.ignore_paths ?? []);
  const [buildArgs, setBuildArgs] = useState("");
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
  const sourceSelect = (
    <GlideSelect
      label="Source"
      value={origin}
      onChange={(value) => {
        setOrigin(value as Origin);
        touch();
      }}
      options={[
        { value: "github", label: "Git provider" },
        { value: "image", label: "Docker image" },
      ]}
      className="w-full"
      triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      ariaLabel="Build source"
      searchable={false}
    />
  );
  const buildMethodSelect = (
    <GlideSelect
      label="Build method"
      value={buildWith}
      onChange={(value) => {
        setBuildWith(value as "railpack" | "dockerfile");
        touch();
      }}
      options={[
        { value: "railpack", label: "Railpack" },
        { value: "dockerfile", label: "Dockerfile" },
      ]}
      className="w-full"
      triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      searchable={false}
    />
  );

  return (
    <Card>
      <CardContent className="space-y-4">
        <SectionHeader
          title="Build source"
          sub="Choose a repository to build or an image to run."
        />
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
              watch_paths: nowBuilds ? watchPaths : [],
              ignore_paths: nowBuilds ? ignorePaths : [],
              ...(source === "dockerfile"
                ? {
                    build_args: Object.fromEntries(
                      buildArgs
                        .split("\n")
                        .map((line) => line.split("="))
                        .filter(([key, value]) => key?.trim() && value !== undefined)
                        .map(([key, ...value]) => [key.trim(), value.join("=")]),
                    ),
                  }
                : {}),
            });
          }}
        >
          {origin === "github" ? (
            <div className="space-y-5">
              <GitHubSource
                sourceControl={sourceSelect}
                buildControl={buildMethodSelect}
                repo={repo}
                gitRef={gitRef}
                onRepo={(url, defaultBranch) => {
                  setRepo(url);
                  // Choosing a new repository makes the old branch meaningless.
                  setGitRef(defaultBranch);
                  touch();
                }}
                onRef={(v) => {
                  setGitRef(v);
                  touch();
                }}
              />
              <div className="space-y-5">
                <PatternField
                  label="Watch patterns"
                  placeholder="services/api/**"
                  patterns={watchPaths}
                  onChange={(next) => {
                    setWatchPaths(next);
                    touch();
                  }}
                />
                <PatternField
                  label="Ignore paths"
                  placeholder="docs/**"
                  patterns={ignorePaths}
                  onChange={(next) => {
                    setIgnorePaths(next);
                    touch();
                  }}
                />
              </div>

              {buildWith === "dockerfile" && (
                <div className="space-y-5">
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
                  <div className="space-y-2">
                    <label
                      htmlFor="dockerfile-build-args"
                      className="text-xs text-muted-foreground"
                    >
                      Dockerfile build arguments
                    </label>
                    <Textarea
                      className="min-h-24 rounded-lg px-3 py-2 font-mono text-sm"
                      value={buildArgs}
                      onChange={(e) => {
                        setBuildArgs(e.target.value);
                        touch();
                      }}
                      placeholder="NODE_ENV=production"
                      id="dockerfile-build-args"
                      aria-label="Dockerfile build arguments"
                    />
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-5">
              <ImageSource
                sourceControl={sourceSelect}
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

function PatternField({
  label,
  placeholder,
  patterns,
  onChange,
}: {
  label: string;
  placeholder: string;
  patterns: string[];
  onChange: (patterns: string[]) => void;
}) {
  const id = useId();
  const [draft, setDraft] = useState("");

  return (
    <div className="space-y-2">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {label}
      </Label>
      <Input
        id={id}
        value={draft}
        placeholder={placeholder}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key !== "Enter") return;
          event.preventDefault();
          const pattern = draft.trim();
          if (!pattern) return;
          if (!patterns.includes(pattern)) onChange([...patterns, pattern]);
          setDraft("");
        }}
        className="h-10 px-3 text-sm"
      />
      {patterns.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {patterns.map((pattern) => (
            <Badge
              key={pattern}
              variant="secondary"
              className="h-7 gap-1.5 px-2.5 font-mono select-none"
            >
              {pattern}
              <button
                type="button"
                className="rounded-full p-0.5 text-muted-foreground transition-colors hover:bg-background/70 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                aria-label={`Remove ${pattern}`}
                onClick={() => onChange(patterns.filter((value) => value !== pattern))}
              >
                <XIcon className="size-3" />
              </button>
            </Badge>
          ))}
        </div>
      )}
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
