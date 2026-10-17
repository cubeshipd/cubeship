"use client";

import {
  ArrowLeftIcon,
  BracesIcon,
  CpuIcon,
  EyeIcon,
  EyeOffIcon,
  GitBranchIcon,
  LayoutDashboardIcon,
  NetworkIcon,
  PencilIcon,
  PlusIcon,
  TerminalSquareIcon,
  Trash2Icon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AppAddresses } from "@/components/app-addresses";
import { AppConfiguration, type AppConfigurationSection } from "@/components/app-configuration";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogsDialog, LogView } from "@/components/container-logs";
import { CopyButton } from "@/components/copy-button";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import { LoadingList } from "@/components/loading";
import { MetricsSection } from "@/components/metrics-section";
import Link from "@/components/navigation-link";
import { Notice } from "@/components/notice";
import { useAppWorkspaceActions } from "@/components/project-workspace";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchBar } from "@/components/search-bar";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { AppShell } from "@/components/terminal-panel";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  type App,
  api,
  can,
  canShell,
  type Deployment,
  type EnvView,
  type ResolvedVar,
} from "@/lib/api";
import { message } from "@/lib/errors";

// One app, under the environment it lives in — which is the only place
// it means anything. `gateway` is unique in acme/api/production and
// nowhere else, so the URL is the reference and the reference is the
// URL.
//
// **Tabs, not one column of sections.** The environment and configuration
// can each be large enough to bury the overview, while logs and shell are
// transient tools that open in dialogs from Actions.
//
// PageProps comes from Next's generated route types, so the params this
// destructures are the segments the directory actually has. Spelling one
// that is not there is a build error rather than an `undefined` glued
// into the reference — which is how this page once asked the daemon for
// /apps/undefined/<project>/<env>/<app> and got "no such endpoint".
export default function AppPage({ params }: PageProps<"/projects/[project]/[env]/[app]">) {
  const { project, env, app } = use(params);
  // The three segments travel separately as well as joined: they are
  // what lets the page draw its own heading before the daemon has said
  // anything, which is the difference between a navigation that feels
  // instant and one that blinks.
  return <Detail reference={`${project}/${env}/${app}`} project={project} env={env} />;
}

type Tab = "overview" | "environment" | AppConfigurationSection;

const tabAliases: Record<string, Tab> = {
  overview: "overview",
  environment: "environment",
  build: "build",
  source: "build",
  deploy: "build",
  network: "network",
  resources: "resources",
  volumes: "resources",
  danger: "resources",
};

// The one reason a deploy's record cannot be removed: the daemon is
// still writing to it.
const stillRunning = "This deploy is still running. Wait for it to finish.";

function Detail({ reference, project, env }: { reference: string; project: string; env: string }) {
  const router = useRouter();
  const me = useSession();
  const { setActions } = useAppWorkspaceActions();
  const [app, setApp] = useState<App | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("overview");
  // Bumped by Deploy so the deployment list reloads after it completes.
  const [deployed, setDeployed] = useState(0);
  const [deploying, setDeploying] = useState(false);
  const [deployError, setDeployError] = useState<string | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [stopOpen, setStopOpen] = useState(false);
  const [starting, setStarting] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [logsOpen, setLogsOpen] = useState(false);
  const [shellOpen, setShellOpen] = useState(false);

  const path = `/apps/${reference}`;
  const reload = useCallback(() => {
    if (!reference) return;
    api
      .get<App>(path)
      .then(setApp)
      .catch((e) => setError(message(e)));
  }, [path, reference]);
  const onDeployed = useCallback(() => {
    setDeployed((n) => n + 1);
    setTab("overview");
    reload();
  }, [reload]);
  const deploy = useCallback(async () => {
    if (deploying) return;
    setDeploying(true);
    setDeployError(null);
    try {
      await api.post(`/apps/${reference}/deploy`, {});
      onDeployed();
    } catch (cause) {
      setDeployError(message(cause));
    } finally {
      setDeploying(false);
    }
  }, [deploying, onDeployed, reference]);
  const deleteApp = useCallback(() => {
    setTab("resources");
    setDeleteOpen(true);
  }, []);
  const requestStop = useCallback(() => {
    setActionError(null);
    setStopOpen(true);
  }, []);
  const startApp = useCallback(async () => {
    setStarting(true);
    setActionError(null);
    try {
      setApp(await api.post<App>(`${path}/start`, {}));
    } catch (cause) {
      setActionError(message(cause));
    } finally {
      setStarting(false);
    }
  }, [path]);
  const canManageApp = can(me, "apps", "manage");
  const openLogs = useCallback(() => setLogsOpen(true), []);
  const openShell = useCallback(() => setShellOpen(true), []);
  useEffect(() => {
    setActions({
      deploy,
      deleteApp,
      logs: app?.has_container ? openLogs : undefined,
      shell: app?.has_container && canShell(me, project) ? openShell : undefined,
      stop: requestStop,
      start: startApp,
      canStop: canManageApp && app?.status === "running",
      canStart: canManageApp && app?.status === "stopped",
      deploying,
      starting,
      deployError,
      actionError,
    });
    return () => setActions({});
  }, [
    app?.has_container,
    app?.status,
    actionError,
    canManageApp,
    deleteApp,
    deploy,
    deployError,
    deploying,
    me,
    openLogs,
    openShell,
    project,
    requestStop,
    setActions,
    startApp,
    starting,
  ]);
  useEffect(reload, [reload]);
  useEffect(() => {
    if (typeof window === "undefined") return;
    const requested = new URLSearchParams(window.location.search).get("tab");
    const next = requested ? tabAliases[requested] : undefined;
    if (next) setTab(next);
  }, []);

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
  if (error) return <ErrorAlert error={error} />;

  // Where this app came from. Built from the URL rather than from the
  // answer, so the way back is there before the answer is.
  const _environment = `/projects/${project}/${env}`;

  // Whether this app runs somewhere other than this box, which changes
  // what this screen can show: its log and its charts are that
  // machine's.
  const remote = app !== null && !app.nodes.includes("control-plane");
  // And whether it runs on more than one, which changes what a log is:
  // there is one per machine and no combined one. The machine its
  // traffic arrives at goes first, because that is the one the daemon
  // answers with when nobody names one.
  const servers = app === null ? [] : app.nodes;

  return (
    <>
      {" "}
      {/* Drawn from the URL, so it is on screen the moment you navigate
          here. This used to return null until the app arrived, which
          blanked the content area on every route change and then
          filled it — a page that disappears before it appears reads as
          slow however fast the request was. */}
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Link
          href={_environment}
          className="inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
        >
          <ArrowLeftIcon className="size-4" />
          Back to apps
        </Link>
      </div>
      {!app && <LoadingList rows={5} />}
      {app && (
        <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="subrail-page">
          {/* The line variant: labels on a rule with the live one lit,
              rather than a filled box that draws more of itself than
              what it switches. See globals.css. */}
          <div className="app-tab-navigation">
            <TabsList variant="default" className="app-tab-list">
              <TabsTrigger value="overview">
                <LayoutDashboardIcon />
                Overview
              </TabsTrigger>
              <TabsTrigger value="environment">
                <BracesIcon />
                Environment
              </TabsTrigger>
              <TabsTrigger value="build">
                <GitBranchIcon />
                Build
              </TabsTrigger>
              <TabsTrigger value="network">
                <NetworkIcon />
                Networking
              </TabsTrigger>
              <TabsTrigger value="resources">
                <CpuIcon />
                Resources
              </TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="overview">
            {/* Where it runs, said once and only when it is somewhere
                else. It also explains the empty charts under it: what
                samples a container is the daemon on the machine it is
                on, and only this one writes to the series. */}
            {/* Two versions answering at one name. It is a fact
                rather than a fault — every rollout across several
                machines passes through it — so it says which machine is
                on what and lets the reader decide, rather than colouring
                itself as an alarm. What makes it worth showing is the
                rollout that never finishes: each replica is running
                something, so without this the app reads as healthy. */}
            {app?.split && (
              <Notice tone="warning">
                This app is answering with more than one version:{" "}
                {app.replicas
                  .filter((r) => r.deploy)
                  .map((r) => `${r.node} is on #${r.deploy}`)
                  .join(", ")}
                . A deploy still rolling out looks like this until the last machine has it; one that
                stopped does not, and the deploy history says which.
              </Notice>
            )}
            {app !== null && app.nodes.length > 1 ? (
              <Notice>
                This app runs on {app.nodes.length} machines —{" "}
                <code className="text-foreground">{app.nodes.join(", ")}</code> — and its traffic
                arrives at this instance, which spreads it across them over the cluster&apos;s
                private network. The charts below are the average across its copies; each machine
                keeps its own log.
              </Notice>
            ) : (
              remote && (
                <Notice>
                  This app runs on <code className="text-foreground">{app.nodes.join(", ")}</code>.
                  Its charts and its log come from there; its traffic still arrives at this
                  instance.
                </Notice>
              )
            )}
            <MetricsSection path={path} />
            {app !== null && <AppAddresses app={app} />}
            <Deployments reference={reference} deployed={deployed} onSettled={reload} />
          </TabsContent>

          <TabsContent value="environment">
            <EnvVars reference={reference} />
          </TabsContent>

          <TabsContent value="build">
            {tab === "build" && (
              <AppConfiguration
                reference={reference}
                app={app}
                section="build"
                deleteOpen={deleteOpen}
                onDeleteOpenChange={setDeleteOpen}
                onAppChange={setApp}
                onDeleted={() => router.push(_environment)}
              />
            )}
          </TabsContent>

          <TabsContent value="network">
            {tab === "network" && (
              <AppConfiguration
                reference={reference}
                app={app}
                section="network"
                deleteOpen={deleteOpen}
                onDeleteOpenChange={setDeleteOpen}
                onAppChange={setApp}
                onDeleted={() => router.push(_environment)}
              />
            )}
          </TabsContent>

          <TabsContent value="resources">
            {tab === "resources" && (
              <AppConfiguration
                reference={reference}
                app={app}
                section="resources"
                deleteOpen={deleteOpen}
                onDeleteOpenChange={setDeleteOpen}
                onAppChange={setApp}
                onDeleted={() => router.push(_environment)}
              />
            )}
          </TabsContent>
        </Tabs>
      )}
      <ContainerLogsDialog
        open={logsOpen}
        onOpenChange={setLogsOpen}
        path={path}
        name={app?.name ?? ""}
        hasContainer={!!app?.has_container}
        servers={servers}
      />
      <Dialog open={shellOpen} onOpenChange={setShellOpen}>
        <DialogContent className="w-[calc(100vw-2rem)] max-w-6xl sm:max-w-6xl">
          <DialogTitle className="flex items-center gap-2 pr-8 font-mono text-sm">
            <span className="sr-only">Shell</span>
            <TerminalSquareIcon className="size-5 shrink-0" aria-hidden="true" />
            <span>{app?.name}</span>
          </DialogTitle>
          {shellOpen && app?.has_container && canShell(me, project) && (
            <AppShell path={path} servers={servers} />
          )}
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={stopOpen}
        onOpenChange={setStopOpen}
        title="Stop this container?"
        confirmLabel="Stop"
        description="Stops every replica and keeps the app and its data. It stays stopped until you start it again."
        onConfirm={async () => {
          setApp(await api.post<App>(`${path}/stop`, {}));
        }}
      />
    </>
  );
}

// Deploying from Actions, with nothing to fill in.
//
// **No tag field.** What a deploy runs is the app's source and the ref
// it is configured with — a branch for something built here, a tag for
// an image — and that is a decision with consequences, made in the
// app's settings where it is written down and stays written down.
// Typing a branch beside the button made every deploy a fresh decision
// about which code this app is, taken in the moment and remembered
// nowhere.
function Deployments({
  reference,
  deployed,
  onSettled,
}: {
  reference: string;
  // Bumped when the header deploys, which is the one thing that changes
  // this list from outside it.
  deployed: number;
  onSettled: () => void;
}) {
  const [list, setList] = useState<Deployment[] | null>(null);
  const [open, setOpen] = useState<Deployment | null>(null);
  const [deleting, setDeleting] = useState<Deployment | null>(null);
  // The app's own name, which is what the confirmation asks somebody to
  // type before it takes the app off the air.
  const name = reference.split("/").pop() ?? reference;
  const [error, setError] = useState<string | null>(null);

  const path = `/apps/${reference}/deployments`;
  const reload = useCallback(() => {
    api
      .get<Deployment[]>(path)
      .then(setList)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // A bump from Deploy tells this list there is a new row to fetch.
  useEffect(() => {
    if (deployed === 0) return;
    reload();
  }, [deployed, reload]);

  // A deploy runs detached, so the row is the only place its outcome
  // appears. Poll while one is in flight and stop when none is.
  useEffect(() => {
    if (!list?.some((d) => d.status === "pending" || d.status === "deploying")) return;
    const timer = setInterval(() => {
      reload();
      onSettled();
    }, 2000);
    return () => clearInterval(timer);
  }, [list, reload, onSettled]);

  const columns: Column<Deployment>[] = [
    {
      id: "status",
      header: "Status",
      width: 16,
      sortBy: (d) => d.status,
      // A stalled deploy is still `pending` — that is what lets a
      // machine that comes back finish the rollout it missed — so the
      // badge is the truth and the sentence beside it is the rest of
      // it: pending on its own reads as "happening", and this one is
      // not happening.
      cell: (d) => (
        <div className="space-y-1">
          <StatusBadge value={d.status} />
          {d.stalled_on && d.stalled_on.length > 0 && (
            <div className="font-mono text-[10px] text-warning">
              waiting on {d.stalled_on.join(", ")}
            </div>
          )}
        </div>
      ),
    },
    {
      id: "image",
      header: "Image",
      width: 56,
      // The full failure detail belongs with the build output in the
      // deployment dialog, not repeated as a paragraph in every row.
      wrap: true,
      cell: (d) => (
        <div className="font-mono text-xs break-all text-muted-foreground">{d.image}</div>
      ),
    },
    {
      id: "when",
      header: "When",
      width: 20,
      sortBy: (d) => d.created_at,
      cell: (d) => (
        <span className="text-xs text-muted-foreground">
          {new Date(d.created_at).toLocaleString()}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 8,
      align: "right",
      // Disabled with the reason rather than hidden: a missing button
      // explains nothing, and "why can I not clear this one" is the
      // question somebody has while looking straight at it.
      cell: (d) => (
        <RowActions>
          <RowAction
            icon={Trash2Icon}
            label={d.live ? `Take the app down` : `Delete deploy ${d.id}`}
            danger={d.deletable}
            disabled={!d.deletable}
            title={
              d.deletable
                ? d.live
                  ? "This is what the app is running. Deleting it takes the app down."
                  : d.stalled_on && d.stalled_on.length > 0
                    ? `Nothing is coming for this: ${d.stalled_on.join(", ")} stopped answering. Deleting it means the machine that comes back runs the deploy below this one.`
                    : undefined
                : stillRunning
            }
            onClick={() => setDeleting(d)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <SectionHeader title="Deployments" />
      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={list}
        rowKey={(d) => String(d.id)}
        loadingRows={3}
        maxHeight="50vh"
        empty="Nothing deployed yet."
        // Every row opens, not only the ones that built something. What
        // is behind it is the deploy — what ran, when, why it stopped
        // and what it printed — and a row that opened for some deploys
        // and not others would be a table you have to guess at.
        onRowClick={setOpen}
      />

      <DeploymentDialog
        reference={reference}
        deployment={open}
        onOpenChange={(shown) => !shown && setOpen(null)}
      />

      {/* Holding confirms removal of the app record or stopping a live app. */}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(shown) => !shown && setDeleting(null)}
        title={deleting?.live ? "Take the app down?" : "Delete this record?"}
        holdToConfirm
        confirmLabel={deleting?.live ? "Take it down" : "Delete"}
        description={
          deleting?.live ? (
            <>
              This is what <code className="text-foreground">{name}</code> is running. Its container
              is stopped and removed, and the app stops answering.{" "}
              <strong>The app itself stays</strong> — its domains, its environment and everything
              attached to it — and the next deploy brings it back.
            </>
          ) : (
            <>
              The row goes, and the build log with it. <strong>The app is not touched</strong> —
              this is the history of a deploy, not anything running now.
            </>
          )
        }
        onConfirm={async () => {
          if (!deleting) return;
          await api.del(`${path}/${deleting.id}`);
          setDeleting(null);
          reload();
          onSettled();
        }}
      />
    </>
  );
}

// One deploy, opened.
//
// It fetches the deployment again rather than showing the row it was
// opened from, because the row does not carry the log: a build's output
// is capped at 256 KiB and the listing is fifty rows, so the daemon
// hands it over one deployment at a time. While the deploy is still
// running it asks again on a timer, which is what makes this the place
// to watch a build from.
function DeploymentDialog({
  reference,
  deployment,
  onOpenChange,
}: {
  reference: string;
  // The row that was clicked, or null when nothing is open.
  deployment: Deployment | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [full, setFull] = useState<Deployment | null>(null);
  const [error, setError] = useState<string | null>(null);

  const id = deployment?.id;
  const load = useCallback(() => {
    if (!id) return;
    api
      .get<Deployment>(`/apps/${reference}/deployments/${id}`)
      .then((d) => {
        setFull(d);
        setError(null);
      })
      .catch((e) => setError(message(e)));
  }, [reference, id]);

  useEffect(() => {
    setFull(null);
    load();
  }, [load]);

  // A build writes its output as it goes, so this is the one screen
  // where re-reading under somebody is the point rather than a
  // discourtesy — and it stops the moment the deploy settles.
  const running = (full ?? deployment)?.status === "pending";
  useEffect(() => {
    if (!id || !running) return;
    const timer = setInterval(load, 2000);
    return () => clearInterval(timer);
  }, [id, running, load]);

  const shown = full ?? deployment;
  const buildLogs = full
    ? [
        full.logs?.trimEnd(),
        full.error && !full.logs?.includes(full.error)
          ? `\u001b[31mDeployment error: ${full.error}\u001b[0m`
          : "",
      ]
        .filter(Boolean)
        .join("\n\n") || null
    : null;

  // Focus lands on the panel rather than on the first thing in it.
  // The first thing in it is the log's filter, and a dialog opened to
  // read a build's output should not open with a cursor blinking in a
  // field: it is what a dialog that wants typing looks like, and here
  // there is nothing to type until somebody has read something.
  const panel = useRef<HTMLDivElement>(null);

  return (
    <Dialog open={deployment !== null} onOpenChange={onOpenChange}>
      <DialogContent ref={panel} initialFocus={panel} className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-3">
            Deploy
            {shown && <StatusBadge value={shown.status} />}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-3 py-4">
          <ErrorAlert error={error} />

          {shown && (
            <div className="flex flex-wrap items-center gap-x-6 gap-y-1 font-mono text-xs text-muted-foreground">
              <span className="break-all">{shown.image || "—"}</span>
              <span>{shown.phase}</span>
              <span>{Math.round(shown.duration_ms / 1000)}s</span>
              <span>{new Date(shown.created_at).toLocaleString()}</span>
            </div>
          )}

          {shown && !shown.cancelled && shown.status === "pending" && (
            <Button
              variant="destructive"
              size="sm"
              onClick={async () => {
                await api.post(`/apps/${reference}/deployments/${shown.id}/cancel`);
                await load();
              }}
            >
              Cancel deploy
            </Button>
          )}

          <LogView
            text={buildLogs}
            busy={full === null}
            name={`${reference.split("/").pop()}-deploy-${deployment?.id ?? ""}`}
            follow={running}
            empty={
              running
                ? "Waiting for the build to say something."
                : "This deploy printed nothing — it ran an image somebody else built rather than building one."
            }
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}

// One variable's value: hidden until asked for, copyable either way.
//
// Hidden by default because this table is most of a page of secrets —
// an app's environment is where its database password, its API keys and
// its signing secrets all end up — and it is the section somebody
// scrolls past with a screen share running. The copy button works while
// it is hidden, which is what makes hiding it cost nothing.
function SecretValue({ value }: { value: string }) {
  const [shown, setShown] = useState(false);
  return (
    <span className="flex items-center gap-1">
      <span className="min-w-0 flex-1 truncate font-mono text-xs">
        {shown ? value : "•".repeat(Math.min(value.length, 24)) || "—"}
      </span>
      <Button
        variant="ghost"
        size="xs"
        aria-label={shown ? "Hide this value" : "Reveal this value"}
        className="shrink-0 text-muted-foreground"
        onClick={() => setShown(!shown)}
      >
        {shown ? <EyeOffIcon className="size-3.5" /> : <EyeIcon className="size-3.5" />}
      </Button>
      <CopyButton value={value} label="Copy this value" className="shrink-0" />
    </span>
  );
}

// What the app's container runs with, and where each value came from.
//
// The filter keeps a long list searchable; the table stays in the page
// flow so every variable can be scanned with the page's normal scroll.
function EnvVars({ reference }: { reference: string }) {
  const [view, setView] = useState<EnvView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [sourceFilter, setSourceFilter] = useState("");
  const [editing, setEditing] = useState<ResolvedVar | null>(null);
  const [adding, setAdding] = useState(false);
  const [unsetting, setUnsetting] = useState<ResolvedVar | null>(null);

  const path = `/apps/${reference}/env`;
  const reload = useCallback(() => {
    api
      .get<EnvView>(path)
      .then(setView)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  const all = view?.effective ?? null;
  const sourceOptions = useMemo(() => {
    const labels: Record<string, string> = {
      app: "App",
      datastore: "Database",
      environment: "Environment",
      objectstore: "Object storage",
      project: "Project",
    };
    const sources = [...new Set((all ?? []).map((variable) => variable.source))].sort((a, b) =>
      (labels[a] ?? a).localeCompare(labels[b] ?? b),
    );
    return [
      { value: "", label: "All sources" },
      ...sources.map((source) => ({ value: source, label: labels[source] ?? source })),
    ];
  }, [all]);
  const rows = useMemo(() => {
    if (all === null) return null;
    const needle = filter.trim().toLowerCase();
    return all.filter(
      (variable) =>
        (!needle || variable.key.toLowerCase().includes(needle)) &&
        (!sourceFilter || variable.source === sourceFilter),
    );
  }, [all, filter, sourceFilter]);

  const columns: Column<ResolvedVar>[] = [
    {
      id: "key",
      header: "Name",
      width: 32,
      sortBy: (v) => v.key,
      cell: (v) => <span className="font-mono text-xs break-all">{v.key}</span>,
    },
    {
      id: "value",
      header: "Value",
      width: 44,
      cell: (v) => <SecretValue value={v.value} />,
    },
    {
      id: "source",
      header: "Set at",
      width: 14,
      sortBy: (v) => v.source,
      cell: (v) => (
        <span className="text-xs text-muted-foreground">
          {sourceOptions.find((option) => option.value === v.source)?.label ?? v.source}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      // Only what this app set itself. A variable inherited from the
      // project, the environment or an attached database is not this
      // screen's to change — editing it here would look like it worked
      // and change nothing, and the level that owns it is where it is
      // owned.
      cell: (v) =>
        v.source === "app" ? (
          <RowActions>
            <RowAction icon={PencilIcon} label={`Edit ${v.key}`} onClick={() => setEditing(v)} />
            <RowAction
              icon={Trash2Icon}
              label={`Unset ${v.key}`}
              danger
              onClick={() => setUnsetting(v)}
            />
          </RowActions>
        ) : null,
    },
  ];

  return (
    <>
      <SectionHeader
        title="Environment"
        sub="The project's variables, then the environment's, then any database or bucket attached to this app, then the app's own. Setting one here overrides the levels above it."
        actions={
          <Button onClick={() => setAdding(true)}>
            <PlusIcon />
            Add variable
          </Button>
        }
      />
      <ErrorAlert error={error} />

      <div className="mb-4 grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem]">
        <SearchBar
          value={filter}
          onChange={setFilter}
          placeholder="Filter by name"
          className="mb-0 min-w-0"
          count={
            all && rows
              ? { value: rows.length, singular: "variable", plural: "variables" }
              : undefined
          }
        />
        <GlideSelect
          value={sourceFilter}
          options={sourceOptions}
          onChange={setSourceFilter}
          ariaLabel="Filter variables by source"
          className="w-full"
          triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-md border border-border-strong bg-secondary px-3 text-left text-sm text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        />
      </div>

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(v) => v.key}
        loadingRows={6}
        empty={filter.trim() || sourceFilter ? "Nothing matches these filters." : "No variables."}
      />

      <EnvVarDialog
        path={path}
        open={adding || editing !== null}
        variable={editing}
        onOpenChange={(open) => {
          if (open) return;
          setAdding(false);
          setEditing(null);
        }}
        onSaved={reload}
      />

      {/* A confirmation with no word to type: unsetting is undone by
          setting it again, so the guard only has to stop the misclick.
          It still needs one — the app comes up without the variable on
          its next deploy, which is a thing that happens later and
          elsewhere. */}
      <ConfirmDialog
        open={unsetting !== null}
        onOpenChange={(open) => !open && setUnsetting(null)}
        title="Unset this variable?"
        confirmLabel="Unset"
        description={
          <>
            <code className="text-foreground">{unsetting?.key}</code> goes from this app. Its
            container keeps the value it is running with until it is deployed again — and if a level
            above sets the same name, that one takes over.
          </>
        }
        onConfirm={async () => {
          if (!unsetting) return;
          await api.patch(path, { unset: [unsetting.key] });
          setUnsetting(null);
          reload();
        }}
      />
    </>
  );
}

// Adding one and editing one are the same dialog, because they are the
// same request: PATCH sets a name to a value whether or not it was
// already there.
function EnvVarDialog({
  path,
  open,
  variable,
  onOpenChange,
  onSaved,
}: {
  path: string;
  open: boolean;
  // The variable being edited, or null when adding.
  variable: ResolvedVar | null;
  onOpenChange: (v: boolean) => void;
  onSaved: () => void;
}) {
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setKey(variable?.key ?? "");
    setValue(variable?.value ?? "");
    setError(null);
  }, [open, variable]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.patch(path, { set: { [key.trim()]: value } });
      onSaved();
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{variable ? "Edit variable" : "Add variable"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <TextField
              label="Name"
              value={key}
              autoFocus={!variable}
              spellCheck={false}
              // The name is the identity of the row being edited.
              // Changing it here would set a second variable and leave
              // the first, which is not what "edit" means anywhere.
              disabled={variable !== null}
              onChange={(e) => setKey(e.target.value)}
            />
            <TextField
              label="Value"
              value={value}
              autoFocus={variable !== null}
              spellCheck={false}
              onChange={(e) => setValue(e.target.value)}
              hint="Takes effect on this app's next deploy — a container keeps the environment it was created with."
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!key.trim()}>
              Save
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
