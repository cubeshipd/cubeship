"use client";

import { useQueryClient } from "@tanstack/react-query";
import {
  BracesIcon,
  CpuIcon,
  FileTextIcon,
  GitBranchIcon,
  LayoutDashboardIcon,
  NetworkIcon,
  PauseIcon,
  PlayIcon,
  RocketIcon,
  TerminalSquareIcon,
  Trash2Icon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { AppAddresses } from "@/components/app-addresses";
import { AppConfiguration, type AppConfigurationSection } from "@/components/app-configuration";
import { Deployments } from "@/components/app-deployments";
import { EnvVars } from "@/components/app-env-vars";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogsDialog } from "@/components/container-logs";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { MetricsSection } from "@/components/metrics-section";
import Link from "@/components/navigation-link";
import { Notice } from "@/components/notice";
import { type MenuItem, ResourceHeader } from "@/components/resource-header";
import { useSession } from "@/components/session-context";
import { AppShell } from "@/components/terminal-panel";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { type App, api, can, canShell } from "@/lib/api";
import {
  appTagsKey,
  buildDataStaleTime,
  gitBranchRequest,
  gitRepositoriesKey,
  imageTagsKey,
  loadAppTags,
  loadGitRepositories,
  loadImageTags,
  loadRegistryCredentials,
  loadRegistryRepositories,
  registryCredentialsKey,
  registryHostForImage,
  registryRepositoriesKey,
} from "@/lib/build-data";
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
  return (
    <Detail reference={`${project}/${env}/${app}`} project={project} env={env} appSlug={app} />
  );
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

function Detail({
  reference,
  project,
  env,
  appSlug,
}: {
  reference: string;
  project: string;
  env: string;
  appSlug: string;
}) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const me = useSession();
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
  useEffect(reload, [reload]);
  useEffect(() => {
    if (!app) return;

    // Build data is slow to obtain from Git and registry providers. Load
    // it while the operator is reading the app overview so opening Build
    // does not start with a blank repository, image, or tag selector.
    void queryClient.prefetchQuery({
      queryKey: gitRepositoriesKey,
      queryFn: loadGitRepositories,
      staleTime: buildDataStaleTime,
    });

    if (app.source === "railpack" || app.source === "dockerfile") {
      const branches = gitBranchRequest(app.repo ?? "");
      if (branches) {
        void queryClient.prefetchQuery({
          queryKey: branches.key,
          queryFn: branches.load,
          staleTime: buildDataStaleTime,
        });
      }
    }

    if (app.source === "external" && app.image) {
      void queryClient.prefetchQuery({
        queryKey: imageTagsKey(app.image),
        queryFn: () => loadImageTags(app.image ?? ""),
        staleTime: buildDataStaleTime,
      });
      void queryClient
        .prefetchQuery({
          queryKey: registryCredentialsKey,
          queryFn: loadRegistryCredentials,
          staleTime: buildDataStaleTime,
        })
        .then(() => {
          const host = registryHostForImage(app.image ?? "");
          const registry = queryClient
            .getQueryData<Awaited<ReturnType<typeof loadRegistryCredentials>>>(
              registryCredentialsKey,
            )
            ?.find((credential) => credential.host === host);
          if (registry) {
            return queryClient.prefetchQuery({
              queryKey: registryRepositoriesKey(registry.id),
              queryFn: () => loadRegistryRepositories(registry.id),
              staleTime: buildDataStaleTime,
            });
          }
        });
    } else if (app.source === "registry" && app.tag) {
      void queryClient.prefetchQuery({
        queryKey: appTagsKey(app.reference),
        queryFn: () => loadAppTags(app.reference),
        staleTime: buildDataStaleTime,
      });
    }
  }, [app, queryClient]);
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
  const name = app?.name ?? appSlug;
  const menu: MenuItem[] = [
    ...(canManageApp && app?.status === "running"
      ? [
          {
            id: "stop",
            label: "Stop",
            icon: <PauseIcon className="size-4" />,
            onSelect: requestStop,
          },
        ]
      : []),
    ...(canManageApp && app?.status === "stopped"
      ? [
          {
            id: "start",
            label: starting ? "Starting…" : "Start",
            icon: <PlayIcon className="size-4" />,
            onSelect: () => void startApp(),
          },
        ]
      : []),
    ...(app
      ? [
          {
            id: "delete",
            label: "Delete app",
            icon: <Trash2Icon className="size-4" />,
            destructive: true,
            onSelect: deleteApp,
          },
        ]
      : []),
  ];

  return (
    <>
      {/* Drawn from the URL, so it is on screen the moment you navigate
          here. This used to return null until the app arrived, which
          blanked the content area on every route change and then
          filled it — a page that disappears before it appears reads as
          slow however fast the request was. */}
      <ResourceHeader
        crumbs={[
          { label: "Projects", href: "/projects" },
          { label: project, href: `/projects/${project}` },
          { label: env, href: _environment },
        ]}
        title={name}
        status={app?.status}
        menu={menu}
        menuLabel={`Actions for ${name}`}
        actions={
          <>
            {app?.has_container && (
              <Button variant="outline" size="lg" onClick={openLogs}>
                <FileTextIcon />
                Logs
              </Button>
            )}
            {app?.has_container && canShell(me, project) && (
              <Button variant="outline" size="lg" onClick={openShell}>
                <TerminalSquareIcon />
                Shell
              </Button>
            )}
            <Button size="lg" disabled={!app || deploying} onClick={() => void deploy()}>
              <RocketIcon />
              {deploying ? "Deploying…" : "Deploy"}
            </Button>
          </>
        }
      />
      <ErrorAlert error={deployError ?? actionError} />
      {!app && <LoadingList rows={5} />}
      {app && (
        <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="workspace-tabs">
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
            <Deployments
              reference={reference}
              deployed={deployed}
              canDeploy={canManageApp}
              onSettled={reload}
            />
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
