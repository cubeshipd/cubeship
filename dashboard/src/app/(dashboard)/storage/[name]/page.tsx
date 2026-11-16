"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AppWindowIcon,
  CpuIcon,
  FileTextIcon,
  FolderIcon,
  LayoutDashboardIcon,
  PauseIcon,
  PlayIcon,
  PlugIcon,
  Trash2Icon,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogsDialog } from "@/components/container-logs";
import { CopyField } from "@/components/copy-field";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { MetricsSection } from "@/components/metrics-section";
import { ResourceHeader } from "@/components/resource-header";
import { useSession } from "@/components/session-context";
import { Attachments } from "@/components/storage-attachments";
import { Buckets } from "@/components/storage-buckets";
import { ObjectStoreAccess, ObjectStoreResources } from "@/components/storage-configuration";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  ApiError,
  api,
  can,
  type ObjectStore,
  type ObjectStoreCredentials,
  objectStorePath,
} from "@/lib/api";
import { storesQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

// One object store, on one page, in tabs — like a database's, and for
// the same reason.
//
// The page opens on the same two questions every one of these pages
// does: is it working, and how do I reach it. Monitoring is a managed
// store's only — a linked one runs on somebody else's machine, and
// there is nothing here to read a cgroup from. Everything else is a
// place you go into rather than something you read past on the way
// somewhere: the buckets are a file browser, the attached apps are a
// table with its own dialog, and a managed store's log is the whole of
// what SeaweedFS has printed. Stacked they made every visit a scroll
// through the other three.
//
// A linked store gets no Logs tab at all, rather than a dead one. There
// is no container here to have printed anything — the store is
// somewhere else — which is a different thing from a log that does not
// exist yet.
type Tab = "overview" | "buckets" | "apps" | "access" | "resources";

const TABS: Record<string, Tab> = {
  overview: "overview",
  buckets: "buckets",
  apps: "apps",
  access: "access",
  general: "access",
  resources: "resources",
};

export default function ObjectStorePage({ params }: PageProps<"/storage/[name]">) {
  const { name } = use(params);
  return <Detail name={name} />;
}

function Detail({ name }: { name: string }) {
  const client = useQueryClient();
  const router = useRouter();
  const me = useSession();
  const asked = useSearchParams().get("tab");
  const [initialTab] = useState<Tab>(() => TABS[asked ?? ""] ?? "overview");
  const [tab, setTab] = useState<Tab>(initialTab);
  const inventory = useQuery(storesQuery);
  const store = inventory.data?.find((item) => item.name === name) ?? null;
  const error = inventory.error ? message(inventory.error) : null;
  const path = objectStorePath(name);
  const reload = useCallback(
    () => client.invalidateQueries({ queryKey: ["objectstores"] }),
    [client],
  );

  const [logsOpen, setLogsOpen] = useState(false);
  const [stopOpen, setStopOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const canManage = can(me, "storage", "manage");
  const managed = store?.kind === "managed";
  const stopped = store?.status === "stopped" || store?.status === "failed";
  const canStart = canManage && managed && stopped;
  const canStop =
    canManage && managed && !!store?.has_container && !stopped && store?.status !== "provisioning";

  async function power(action: "start" | "stop") {
    setBusy(true);
    setActionError(null);
    try {
      await api.post(`${path}/${action}`);
      await reload();
    } catch (cause) {
      setActionError(message(cause));
    } finally {
      setBusy(false);
      setStopOpen(false);
    }
  }

  // Provisioning is a container being pulled and started, detached from
  // the request that asked for it — so this is the one screen that has
  // to go back and look. It stops the moment it settles.
  useEffect(() => {
    if (store?.status !== "provisioning") return;
    const timer = setInterval(() => void reload(), 2000);
    return () => clearInterval(timer);
  }, [store?.status, reload]);

  if (error && !store) return <ErrorAlert error={error} />;
  if (!inventory.isPending && !store) return <ErrorAlert error="Object storage was not found." />;
  const activeTab = store?.kind !== "managed" && tab === "resources" ? "access" : tab;

  return (
    <>
      <ResourceHeader
        crumbs={[{ label: "Object storage", href: "/storage" }]}
        title={name}
        status={store?.status}
        actions={
          store && (
            <>
              {canStart && (
                <Button disabled={busy} onClick={() => void power("start")}>
                  <PlayIcon />
                  {busy ? "Working…" : "Start"}
                </Button>
              )}
              {canStop && (
                <Button variant="outline" disabled={busy} onClick={() => setStopOpen(true)}>
                  <PauseIcon />
                  {busy ? "Working…" : "Stop"}
                </Button>
              )}
              {managed && store.has_container && (
                <Button variant="outline" onClick={() => setLogsOpen(true)}>
                  <FileTextIcon />
                  Logs
                </Button>
              )}
            </>
          )
        }
        menu={
          canManage && store
            ? [
                {
                  id: "delete",
                  label: "Delete storage",
                  icon: <Trash2Icon className="size-4" />,
                  destructive: true,
                  onSelect: () => setDeleteOpen(true),
                },
              ]
            : []
        }
      />
      <ErrorAlert error={error ?? actionError} />
      {!store && <LoadingList rows={5} />}
      {/* Why it did not come up is the tail of what SeaweedFS printed, and
          it appears nowhere else — the container it came from has been
          removed. */}
      {store?.status === "failed" && store.error && (
        <Card className="mb-4 border-l-2 border-destructive/30 border-l-destructive">
          <CardContent>
            <div className="text-[11px] tracking-[0.12em] text-destructive uppercase">
              It did not start
            </div>
            <pre className="mt-2 overflow-x-auto font-mono text-xs whitespace-pre-wrap text-muted-foreground">
              {store.error}
            </pre>
          </CardContent>
        </Card>
      )}
      {store && (
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
              <TabsTrigger value="buckets">
                <FolderIcon />
                Buckets
              </TabsTrigger>
              <TabsTrigger value="apps">
                <AppWindowIcon />
                Apps
              </TabsTrigger>
              <TabsTrigger value="access">
                <PlugIcon />
                Access
              </TabsTrigger>
              {store.kind === "managed" && (
                <TabsTrigger value="resources">
                  <CpuIcon />
                  Resources
                </TabsTrigger>
              )}
            </TabsList>
          </div>

          <TabsContent value="overview">
            {/* Only for a store this instance runs. A linked one is
                somebody else's server: nothing here samples it, and the
                daemon refuses the endpoint rather than answering with
                an empty series. */}
            {store.kind === "managed" && <MetricsSection path={path} />}
            <Connection store={store} />
          </TabsContent>

          <TabsContent value="buckets">
            <Buckets store={store} />
          </TabsContent>

          <TabsContent value="apps">
            <Attachments store={store} onChanged={reload} />
          </TabsContent>
          <TabsContent value="access">
            <ObjectStoreAccess key={store.name} store={store} onChanged={reload} />
          </TabsContent>
          {store.kind === "managed" && (
            <TabsContent value="resources">
              <ObjectStoreResources store={store} onSaved={reload} />
            </TabsContent>
          )}
        </Tabs>
      )}
      {store && managed && (
        <>
          <ConfirmDialog
            open={stopOpen}
            onOpenChange={setStopOpen}
            title="Stop this store?"
            confirmLabel="Stop"
            description="Apps that read or write to this store will lose access until it starts again."
            onConfirm={() => power("stop")}
          />
          <ContainerLogsDialog
            open={logsOpen}
            onOpenChange={setLogsOpen}
            path={path}
            name={store.name}
            hasContainer={store.has_container}
          />
        </>
      )}
      {store && (
        <ConfirmDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          title="Delete this store?"
          confirmLabel="Delete storage"
          holdToConfirm
          description={
            managed ? (
              <>
                Its container and <strong>every object on this host</strong> will be deleted. There
                is no backup.
              </>
            ) : (
              <>
                This instance forgets the endpoint and account.{" "}
                <strong>The bucket and its objects stay untouched.</strong>
              </>
            )
          }
          onConfirm={async () => {
            await api.del(path);
            await reload();
            setDeleteOpen(false);
            router.replace("/storage");
          }}
        />
      )}
    </>
  );
}

// Everything needed to point something at this store, as fields you
// copy and cannot edit.
//
// The keys are their own request and an admin's. A member sees the
// address and nothing else, which is most of what is here — so the
// refusal is a note rather than an error.
function Connection({ store }: { store: ObjectStore }) {
  const [creds, setCreds] = useState<ObjectStoreCredentials | null>(null);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setCreds(null);
    setForbidden(false);
    api
      .get<ObjectStoreCredentials>(`${objectStorePath(store.name)}/credentials`)
      .then(setCreds)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(message(e));
      });
  }, [store.name]);

  return (
    <Card className="mb-4">
      <CardContent className="space-y-5">
        <header className="space-y-1">
          <h2 className="text-lg font-medium tracking-tight">Connection settings</h2>
          <p className="max-w-2xl text-sm text-muted-foreground">
            {store.kind === "managed"
              ? "Apps on this instance reach the store on the shared network."
              : `${store.provider_label}, reached with the account this store authenticates as.`}
          </p>
        </header>
        <ErrorAlert error={error} />

        <div className="grid gap-4 sm:grid-cols-2">
          <CopyField label="Endpoint" value={store.endpoint} />
          <CopyField label="Region" value={store.region} />
          {creds && (
            <>
              <CopyField label="Access key id" value={creds.access_key} />
              <CopyField label="Secret access key" value={creds.secret_key} masked />
            </>
          )}
          {store.external_endpoint && (
            <CopyField
              label="From outside this host"
              value={store.external_endpoint}
              hint="Published on a host port, without TLS. Restrict access with a firewall rule."
            />
          )}
        </div>

        {forbidden && (
          <p className="text-xs text-subtle-foreground">The keys are an admin's to read.</p>
        )}
        {store.path_style && (
          <p className="text-xs leading-relaxed text-subtle-foreground">
            This endpoint addresses buckets by path. Most clients need telling —{" "}
            <code>--endpoint-url</code> plus <code>--force-path-style</code> for the AWS CLI.
          </p>
        )}
      </CardContent>
    </Card>
  );
}
