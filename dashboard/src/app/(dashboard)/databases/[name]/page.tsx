"use client";

import { useQueryClient } from "@tanstack/react-query";
import {
  AppWindowIcon,
  ArchiveIcon,
  BlocksIcon,
  CpuIcon,
  FileTextIcon,
  LayoutDashboardIcon,
  NetworkIcon,
  PauseIcon,
  PlayIcon,
  Trash2Icon,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogsDialog } from "@/components/container-logs";
import { Attachments } from "@/components/database-attachments";
import { Connection } from "@/components/database-connection";
import { Extensions } from "@/components/database-extensions";
import {
  DatabaseAccess,
  DatabaseBackups,
  DatabaseLimits,
} from "@/components/database-settings-panels";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { MetricsSection } from "@/components/metrics-section";
import { ResourceHeader } from "@/components/resource-header";
import { useSession } from "@/components/session-context";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { api, can, DATASTORE_STOPPED, type Datastore, datastorePath } from "@/lib/api";
import { message } from "@/lib/errors";

// One database, on one page, in tabs — the same shape as an app's.
//
// It was sections for a while, on the grounds that all three were short
// and none was an alternative to the others. Two of them are not short.
// A log is the whole of what the engine has printed, and the attached
// apps are a table with its own dialog and its own confirmations; below
// them the two things somebody opens this page for — is it working, and
// how do I connect to it — sat off the bottom of the screen exactly
// when the database was busy enough to be worth looking at.
//
// So the page opens on what is short and always wanted, and what is
// long, or is somewhere you go rather than something you read, is a tab
// beside it.
type Tab = "overview" | "extensions" | "apps" | "backups" | "access" | "resources";

const TAB_ALIASES: Record<string, Tab> = {
  overview: "overview",
  extensions: "extensions",
  apps: "apps",
  backups: "backups",
  access: "access",
  network: "access",
  resources: "resources",
  danger: "resources",
};

export default function DatastorePage({ params }: PageProps<"/databases/[name]">) {
  const { name } = use(params);
  return <Detail name={name} />;
}

function Detail({ name }: { name: string }) {
  // Which tab is open is view state rather than an identity, so it is
  // not a segment — but it is linkable, because the Backups screen's
  // whole job is "this one is not protected" and landing somebody on
  // Overview to hunt for the tab is the papercut that makes a report
  // not worth opening.
  //
  // **Through `useSearchParams` rather than `window`**, and the
  // difference is not style: this page renders on the server first,
  // where there is no window, so a `useState` initializer reading one
  // produces "overview" — and React keeps the state it hydrated with
  // rather than re-running the initializer, so the parameter was read
  // and silently thrown away. This hook is the one thing that has the
  // same answer on both sides.
  const asked = useSearchParams().get("tab");
  const [initial] = useState<Tab>(() => TAB_ALIASES[asked ?? ""] ?? "overview");
  const router = useRouter();
  const queryClient = useQueryClient();
  const me = useSession();
  const [datastore, setDatastore] = useState<Datastore | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Only the arrival decides it. After that the tab row does, and a
  // stale parameter must not drag somebody back when they click away.
  const [tab, setTab] = useState<Tab>(initial);
  const [stopOpen, setStopOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [logsOpen, setLogsOpen] = useState(false);
  const [powerBusy, setPowerBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const path = datastorePath(name);
  const reload = useCallback(() => {
    api
      .get<Datastore>(path)
      .then(setDatastore)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  const canManage = can(me, "databases", "manage");
  const isStopped = datastore?.status === DATASTORE_STOPPED || datastore?.status === "failed";
  const activeTab = datastore
    ? tab === "backups" && !datastore.can_back_up
      ? "access"
      : tab === "extensions" && datastore.engine !== "postgres"
        ? "overview"
        : tab
    : tab;
  const power = useCallback(
    async (action: "start" | "stop") => {
      if (!datastore) return;
      setPowerBusy(true);
      setActionError(null);
      try {
        await api.post(`${path}/${action}`);
        reload();
        await queryClient.invalidateQueries({ queryKey: ["datastores"] });
      } catch (cause) {
        setActionError(message(cause));
      } finally {
        setPowerBusy(false);
        setStopOpen(false);
      }
    },
    [datastore, path, queryClient, reload],
  );

  // Provisioning is a container being pulled and started, detached from
  // the request that asked for it — so this is the one screen that has
  // to go back and look. It stops the moment it settles.
  useEffect(() => {
    if (datastore?.status !== "provisioning") return;
    const timer = setInterval(reload, 2000);
    return () => clearInterval(timer);
  }, [datastore?.status, reload]);

  if (error && !datastore) return <ErrorAlert error={error} />;

  const canStop =
    canManage && !!datastore?.has_container && !isStopped && datastore.status !== "provisioning";

  return (
    <>
      <ResourceHeader
        crumbs={[{ label: "Databases", href: "/databases" }]}
        title={name}
        status={datastore?.status}
        actions={
          datastore && (
            <>
              {canManage && isStopped && (
                <Button disabled={powerBusy} onClick={() => void power("start")}>
                  <PlayIcon />
                  {powerBusy ? "Working…" : "Start"}
                </Button>
              )}
              {canStop && (
                <Button variant="outline" disabled={powerBusy} onClick={() => setStopOpen(true)}>
                  <PauseIcon />
                  {powerBusy ? "Working…" : "Stop"}
                </Button>
              )}
              {datastore.has_container && (
                <Button variant="outline" onClick={() => setLogsOpen(true)}>
                  <FileTextIcon />
                  Logs
                </Button>
              )}
            </>
          )
        }
        menu={
          canManage && datastore
            ? [
                {
                  id: "delete",
                  label: "Delete database",
                  icon: <Trash2Icon className="size-4" />,
                  destructive: true,
                  onSelect: () => setDeleteOpen(true),
                },
              ]
            : []
        }
      />
      <ErrorAlert error={error ?? actionError} />
      {!datastore && <LoadingList rows={5} />}
      {/* The reason a database did not come up is the tail of what the
          engine printed, and it appears nowhere else — the container it
          came from has been removed. */}
      {datastore?.status === "failed" && datastore.error && (
        <Card className="mb-4 border-l-2 border-destructive/30 border-l-destructive">
          <CardContent>
            <div className="text-[11px] tracking-[0.12em] text-destructive uppercase">
              It did not start
            </div>
            <pre className="mt-2 overflow-x-auto font-mono text-xs whitespace-pre-wrap text-muted-foreground">
              {datastore.error}
            </pre>
          </CardContent>
        </Card>
      )}
      {datastore && (
        <Tabs
          value={activeTab}
          onValueChange={(value) => setTab(value as Tab)}
          className="workspace-tabs"
        >
          <div className="app-tab-navigation">
            <TabsList variant="default" className="app-tab-list">
              <TabsTrigger value="overview">
                <LayoutDashboardIcon />
                Overview
              </TabsTrigger>
              {datastore.engine === "postgres" && (
                <TabsTrigger value="extensions">
                  <BlocksIcon />
                  Extensions
                </TabsTrigger>
              )}
              <TabsTrigger value="apps">
                <AppWindowIcon />
                Apps
              </TabsTrigger>
              {datastore.can_back_up && (
                <TabsTrigger value="backups">
                  <ArchiveIcon />
                  Backups
                </TabsTrigger>
              )}
              <TabsTrigger value="access">
                <NetworkIcon />
                Access
              </TabsTrigger>
              <TabsTrigger value="resources">
                <CpuIcon />
                Resources
              </TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="overview">
            <MetricsSection path={path} />
            <Connection datastore={datastore} />
          </TabsContent>
          {datastore.engine === "postgres" && (
            <TabsContent value="extensions">
              <Extensions datastore={datastore} onChanged={reload} />
            </TabsContent>
          )}
          <TabsContent value="apps">
            <Attachments datastore={datastore} onChanged={reload} />
          </TabsContent>
          {datastore.can_back_up && (
            <TabsContent value="backups">
              <DatabaseBackups datastore={datastore} />
            </TabsContent>
          )}
          <TabsContent value="access">
            <DatabaseAccess key={datastore.name} datastore={datastore} onChanged={reload} />
          </TabsContent>
          <TabsContent value="resources">
            <DatabaseLimits datastore={datastore} onSaved={setDatastore} />
          </TabsContent>
        </Tabs>
      )}
      {datastore && (
        <>
          <ConfirmDialog
            open={stopOpen}
            onOpenChange={setStopOpen}
            title={`Stop ${datastore.name}?`}
            description={
              <>
                The container stops and its data stays where it is — starting it again brings it
                back unchanged.{" "}
                {datastore.attachments.length > 0 ? (
                  <>
                    <strong>
                      {datastore.attachments.map((attachment) => attachment.app).join(", ")}
                    </strong>{" "}
                    will keep running and start failing to connect.
                  </>
                ) : (
                  "No app is attached to it."
                )}
              </>
            }
            confirmLabel="Stop it"
            onConfirm={() => power("stop")}
          />
          <ConfirmDialog
            open={deleteOpen}
            onOpenChange={setDeleteOpen}
            title={`Delete ${datastore.name}?`}
            description={
              <>
                The container is stopped and the data on disk goes with it. Nothing is kept, and{" "}
                {datastore.attachments.length > 0 ? (
                  <>
                    <strong>
                      {datastore.attachments.map((attachment) => attachment.app).join(", ")}
                    </strong>{" "}
                    will come up without a connection string the next time they are deployed.
                  </>
                ) : (
                  "no app is attached to it."
                )}
              </>
            }
            holdToConfirm
            confirmLabel="Delete database"
            onConfirm={async () => {
              await api.del(path);
              await queryClient.invalidateQueries({ queryKey: ["datastores"] });
              router.push("/databases");
            }}
          />
          <ContainerLogsDialog
            open={logsOpen}
            onOpenChange={setLogsOpen}
            path={path}
            name={datastore.name}
            hasContainer={datastore.has_container}
          />
        </>
      )}
    </>
  );
}
