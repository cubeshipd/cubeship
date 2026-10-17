"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ActivityIcon,
  BoxIcon,
  DatabaseIcon,
  FileTextIcon,
  FolderTreeIcon,
  HardDriveIcon,
  LayersIcon,
  MoreHorizontalIcon,
  PauseIcon,
  PlayIcon,
  PlusIcon,
  ShapesIcon,
  TerminalSquareIcon,
  Trash2Icon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogs } from "@/components/container-logs";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import NavigationLink from "@/components/navigation-link";
import { NewDatastoreDialog } from "@/components/new-datastore-dialog";
import { NewWorkloadAppDialog } from "@/components/new-workload-app-dialog";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { AppShell } from "@/components/terminal-panel";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import {
  api,
  type ContainerUsage,
  can,
  canShell,
  datastorePath,
  formatBytes,
  formatCPU,
  objectStorePath,
} from "@/lib/api";
import { appsQuery, containersQuery, datastoresQuery, storesQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

type Workload = {
  id: string;
  reference?: string;
  kind: ContainerUsage["kind"];
  name: string;
  status: string;
  href: string;
  logsPath: string;
  usage?: ContainerUsage;
  servers: string[];
  shell: boolean;
  scopes: { project: string; environment: string }[];
};

const labels: Record<Workload["kind"], string> = {
  app: "App",
  datastore: "Database",
  objectstore: "Object storage",
};

const filterSelectTriggerClass =
  "flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-md border px-3 text-left text-sm text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 disabled:cursor-not-allowed disabled:opacity-50";

function formatAge(startedAt?: string): string {
  if (!startedAt) return "—";
  const elapsed = Date.now() - Date.parse(startedAt);
  if (!Number.isFinite(elapsed) || elapsed < 0) return "—";
  const minutes = Math.floor(elapsed / 60_000);
  if (minutes < 1) return "<1m";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

export default function WorkloadsPage() {
  const me = useSession();
  const router = useRouter();
  const queryClient = useQueryClient();
  const [logs, setLogs] = useState<Workload | null>(null);
  const [shell, setShell] = useState<Workload | null>(null);
  const [stopping, setStopping] = useState<Workload | null>(null);
  const [deleting, setDeleting] = useState<Workload | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [creating, setCreating] = useState<"app" | "database" | null>(null);
  const [project, setProject] = useState("");
  const [environment, setEnvironment] = useState("");
  const [kind, setKind] = useState("");
  const [status, setStatus] = useState("");
  const apps = useQuery({ ...appsQuery, enabled: can(me, "apps") });
  const datastores = useQuery({ ...datastoresQuery, enabled: can(me, "databases") });
  const stores = useQuery({ ...storesQuery, enabled: can(me, "storage") });
  const readings = useQuery({ ...containersQuery, enabled: can(me, "servers") });
  const errors = [apps, datastores, stores, readings]
    .filter((query) => query.isError)
    .map((query) => message(query.error));
  const usage = useMemo(
    () =>
      new Map((readings.data ?? []).map((reading) => [`${reading.kind}:${reading.name}`, reading])),
    [readings.data],
  );
  const projectChoices = useMemo(
    () => [
      { value: "", label: "All projects" },
      ...[...new Set((apps.data ?? []).map((app) => app.project))]
        .sort()
        .map((slug) => ({ value: slug, label: slug })),
    ],
    [apps.data],
  );
  const environmentChoices = useMemo(
    () => [
      { value: "", label: "All environments" },
      ...[
        ...new Set(
          (apps.data ?? [])
            .filter((app) => !project || app.project === project)
            .map((app) => app.environment),
        ),
      ]
        .sort()
        .map((slug) => ({ value: slug, label: slug })),
    ],
    [apps.data, project],
  );
  const rows = useMemo(() => {
    const workloads: Workload[] = [];
    for (const app of apps.data ?? []) {
      if (!app.has_container) continue;
      const path = `/apps/${app.reference}`;
      workloads.push({
        id: app.reference,
        reference: app.reference,
        kind: "app",
        name: app.name,
        status: app.status,
        href: `/projects/${app.reference}`,
        logsPath: path,
        usage: usage.get(`app:${app.reference}`),
        servers: app.nodes,
        shell: canShell(me, app.project),
        scopes: [{ project: app.project, environment: app.environment }],
      });
    }
    for (const db of datastores.data ?? []) {
      if (!db.has_container) continue;
      const relatedApps = (apps.data ?? []).filter((app) =>
        db.attachments.some((attachment) => attachment.app === app.reference),
      );
      workloads.push({
        id: `datastore:${db.name}`,
        kind: "datastore",
        name: db.name,
        status: db.status,
        href: `/databases/${db.name}`,
        logsPath: `/datastores/${db.name}`,
        usage: usage.get(`datastore:${db.name}`),
        servers: [],
        shell: false,
        scopes: relatedApps.map((app) => ({ project: app.project, environment: app.environment })),
      });
    }
    for (const store of stores.data ?? []) {
      if (!store.has_container) continue;
      const relatedApps = (apps.data ?? []).filter((app) =>
        store.attachments.some((attachment) => attachment.app === app.reference),
      );
      workloads.push({
        id: `objectstore:${store.name}`,
        kind: "objectstore",
        name: store.name,
        status: store.status,
        href: `/storage/${store.name}`,
        logsPath: `/objectstores/${store.name}`,
        usage: usage.get(`objectstore:${store.name}`),
        servers: [],
        shell: false,
        scopes: relatedApps.map((app) => ({ project: app.project, environment: app.environment })),
      });
    }
    return workloads;
  }, [apps.data, datastores.data, stores.data, usage, me]);

  const statusChoices = [
    { value: "", label: "All statuses" },
    ...[...new Set(rows.map((row) => row.status))].sort().map((value) => ({ value, label: value })),
  ];
  const filteredRows = rows.filter(
    (row) =>
      (!kind || row.kind === kind) &&
      (!status || row.status === status) &&
      ((!project && !environment) ||
        row.scopes.some(
          (scope) =>
            (!project || scope.project === project) &&
            (!environment || scope.environment === environment),
        )),
  );

  const columns: Column<Workload>[] = [
    {
      id: "name",
      header: "Container",
      width: 24,
      sortBy: (row) => row.name,
      cell: (row) => {
        const Icon =
          row.kind === "app" ? BoxIcon : row.kind === "datastore" ? DatabaseIcon : HardDriveIcon;
        return (
          <span className="flex min-w-0 items-center gap-3">
            <span
              role="img"
              aria-label={labels[row.kind]}
              title={labels[row.kind]}
              className="inline-flex w-5 shrink-0 justify-center text-muted-foreground"
            >
              <Icon className="size-4" aria-hidden="true" />
            </span>
            <span className="truncate font-mono text-sm">{row.name}</span>
          </span>
        );
      },
    },
    {
      id: "project",
      header: "Project",
      width: 14,
      sortBy: (row) => [...new Set(row.scopes.map((scope) => scope.project))].sort().join(", "),
      cell: (row) =>
        [...new Set(row.scopes.map((scope) => scope.project))].sort().join(", ") || "—",
    },
    {
      id: "environment",
      header: "Environment",
      width: 14,
      sortBy: (row) => [...new Set(row.scopes.map((scope) => scope.environment))].sort().join(", "),
      cell: (row) =>
        [...new Set(row.scopes.map((scope) => scope.environment))].sort().join(", ") || "—",
    },
    {
      id: "status",
      header: "Status",
      width: 12,
      sortBy: (row) => row.status,
      cell: (row) => <StatusBadge value={row.status} />,
    },
    {
      id: "age",
      header: "Age",
      width: 9,
      align: "right",
      sortBy: (row) => row.usage?.started_at ?? "",
      cell: (row) => {
        const startedAt = row.usage?.started_at;
        return (
          <time
            dateTime={startedAt}
            title={startedAt ? new Date(startedAt).toLocaleString() : undefined}
            className="font-mono text-sm"
          >
            {formatAge(startedAt)}
          </time>
        );
      },
    },
    {
      id: "cpu",
      header: "CPU",
      width: 9,
      align: "right",
      sortBy: (row) => row.usage?.cpu_percent ?? -1,
      cell: (row) => (
        <span className="font-mono text-sm">
          {row.usage ? formatCPU(row.usage.cpu_percent) : "—"}
        </span>
      ),
    },
    {
      id: "memory",
      header: "Memory",
      width: 10,
      align: "right",
      sortBy: (row) => row.usage?.memory_bytes ?? -1,
      cell: (row) => (
        <span className="font-mono text-sm">
          {row.usage ? formatBytes(row.usage.memory_bytes) : "—"}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 8,
      align: "right",
      cell: (row) => (
        <GlideSelect
          value=""
          ariaLabel={`Actions for ${row.reference ?? row.name}`}
          triggerContent={<MoreHorizontalIcon className="size-3.5 text-muted-foreground" />}
          triggerClassName="inline-flex size-7 cursor-pointer items-center justify-center rounded-md border-0 bg-transparent p-0 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
          align="right"
          options={[
            ...(can(
              me,
              row.kind === "app" ? "apps" : row.kind === "datastore" ? "databases" : "storage",
              "manage",
            ) && row.status === "running"
              ? [
                  {
                    value: "stop",
                    label: (
                      <span className="inline-flex items-center gap-2">
                        <PauseIcon className="size-4" />
                        Stop container
                      </span>
                    ),
                  },
                ]
              : can(
                    me,
                    row.kind === "app"
                      ? "apps"
                      : row.kind === "datastore"
                        ? "databases"
                        : "storage",
                    "manage",
                  ) && row.status === "stopped"
                ? [
                    {
                      value: "start",
                      label: (
                        <span className="inline-flex items-center gap-2">
                          <PlayIcon className="size-4" />
                          Start container
                        </span>
                      ),
                    },
                  ]
                : []),
            {
              value: "logs",
              label: (
                <span className="inline-flex items-center gap-2">
                  <FileTextIcon className="size-4" />
                  View logs
                </span>
              ),
            },
            ...(row.shell
              ? [
                  {
                    value: "shell",
                    label: (
                      <span className="inline-flex items-center gap-2">
                        <TerminalSquareIcon className="size-4" />
                        Open shell
                      </span>
                    ),
                  },
                ]
              : []),
            ...(can(
              me,
              row.kind === "app" ? "apps" : row.kind === "datastore" ? "databases" : "storage",
              "manage",
            )
              ? [
                  {
                    value: "delete",
                    label: (
                      <span className="inline-flex items-center gap-2 text-destructive">
                        <Trash2Icon className="size-4" />
                        Delete
                      </span>
                    ),
                  },
                ]
              : []),
          ]}
          onChange={(action) => {
            if (action === "stop") setStopping(row);
            else if (action === "start")
              void start(row).catch((error) => setActionError(message(error)));
            else if (action === "logs") setLogs(row);
            else if (action === "shell") setShell(row);
            else if (action === "delete") setDeleting(row);
          }}
        />
      ),
    },
  ];

  async function refresh() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["apps"] }),
      queryClient.invalidateQueries({ queryKey: ["datastores"] }),
      queryClient.invalidateQueries({ queryKey: ["objectstores"] }),
      queryClient.invalidateQueries({ queryKey: ["instance-containers"] }),
    ]);
  }

  async function start(row: Workload) {
    setActionError(null);
    const path =
      row.kind === "app"
        ? `/apps/${row.reference}`
        : row.kind === "datastore"
          ? datastorePath(row.name)
          : objectStorePath(row.name);
    await api.post(`${path}/start`);
    await refresh();
  }

  async function stop(row: Workload) {
    const path =
      row.kind === "app"
        ? `/apps/${row.reference}`
        : row.kind === "datastore"
          ? datastorePath(row.name)
          : objectStorePath(row.name);
    await api.post(`${path}/stop`);
    await refresh();
  }

  async function remove(row: Workload) {
    const path =
      row.kind === "app"
        ? `/apps/${row.reference}`
        : row.kind === "datastore"
          ? datastorePath(row.name)
          : objectStorePath(row.name);
    await api.del(path);
    await refresh();
  }

  const pending =
    (can(me, "apps") && apps.isPending) ||
    (can(me, "databases") && datastores.isPending) ||
    (can(me, "storage") && stores.isPending);

  return (
    <>
      <SectionHeader
        title="Containers"
        sub="Every app, database and managed object store running on this instance. CPU 100% is one core."
        actions={
          can(me, "apps", "manage") || can(me, "databases", "manage") ? (
            <GlideSelect
              value=""
              ariaLabel="Create a workload"
              placeholder="Create"
              accent
              align="right"
              mobileAlign="left"
              startIcon={<PlusIcon className="size-4" />}
              options={[
                ...(can(me, "apps", "manage")
                  ? [
                      {
                        value: "app",
                        label: (
                          <span className="inline-flex items-center gap-2">
                            <BoxIcon className="size-4 text-muted-foreground" aria-hidden="true" />
                            App
                          </span>
                        ),
                      },
                    ]
                  : []),
                ...(can(me, "databases", "manage")
                  ? [
                      {
                        value: "database",
                        label: (
                          <span className="inline-flex items-center gap-2">
                            <DatabaseIcon
                              className="size-4 text-muted-foreground"
                              aria-hidden="true"
                            />
                            Database
                          </span>
                        ),
                      },
                    ]
                  : []),
              ]}
              onChange={(value) => setCreating(value as "app" | "database")}
            />
          ) : undefined
        }
      />
      <div className="mb-4 grid grid-cols-2 gap-3 rounded-xl border border-border bg-card p-3 xl:grid-cols-4">
        {can(me, "apps") && (
          <>
            <GlideSelect
              label="Project"
              value={project}
              options={projectChoices}
              onChange={(value) => {
                setProject(value);
                setEnvironment("");
              }}
              placeholder="All projects"
              searchable
              startIcon={<FolderTreeIcon className="size-4" />}
              className="w-full"
              triggerClassName={filterSelectTriggerClass}
            />
            <GlideSelect
              label="Environment"
              value={environment}
              options={environmentChoices}
              onChange={setEnvironment}
              placeholder="All environments"
              searchable
              startIcon={<LayersIcon className="size-4" />}
              className="w-full"
              triggerClassName={filterSelectTriggerClass}
            />
          </>
        )}
        <GlideSelect
          label="Type"
          value={kind}
          options={[
            { value: "", label: "All types" },
            ...Object.entries(labels).map(([value, label]) => ({ value, label })),
          ]}
          onChange={setKind}
          placeholder="All types"
          startIcon={<ShapesIcon className="size-4" />}
          className="w-full"
          triggerClassName={filterSelectTriggerClass}
        />
        <GlideSelect
          label="Status"
          value={status}
          options={statusChoices}
          onChange={setStatus}
          placeholder="All statuses"
          searchable={statusChoices.length >= 8}
          startIcon={<ActivityIcon className="size-4" />}
          className="w-full"
          triggerClassName={filterSelectTriggerClass}
        />
      </div>
      <ErrorAlert error={actionError ?? (errors.length ? errors.join(" · ") : null)} />
      <DataTable
        columns={columns}
        rows={pending ? null : filteredRows}
        rowKey={(row) => row.id}
        mobileRow={(row) => {
          const Icon =
            row.kind === "app" ? BoxIcon : row.kind === "datastore" ? DatabaseIcon : HardDriveIcon;
          const scope = [
            ...new Set(row.scopes.map(({ project, environment }) => `${project} / ${environment}`)),
          ];
          const startedAt = row.usage?.started_at;
          return (
            <article className="rounded-lg border border-border bg-background p-3">
              <div className="flex items-center gap-2">
                <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                <NavigationLink
                  href={row.href}
                  className="min-w-0 flex-1 truncate font-mono text-sm font-medium"
                >
                  {row.name}
                </NavigationLink>
                <StatusBadge value={row.status} />
                {columns.find((column) => column.id === "actions")?.cell(row)}
              </div>
              <p
                className={`mt-1 truncate text-xs text-muted-foreground ${scope.length ? "pl-6" : ""}`}
              >
                {scope.length ? scope.join(", ") : labels[row.kind]}
              </p>
              <dl className="mt-3 grid grid-cols-3 gap-2 border-t border-border pt-2 text-xs">
                <div>
                  <dt className="text-muted-foreground">Age</dt>
                  <dd className="mt-0.5 font-mono">{formatAge(startedAt)}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">CPU</dt>
                  <dd className="mt-0.5 font-mono">
                    {row.usage ? formatCPU(row.usage.cpu_percent) : "—"}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Memory</dt>
                  <dd className="mt-0.5 font-mono">
                    {row.usage ? formatBytes(row.usage.memory_bytes) : "—"}
                  </dd>
                </div>
              </dl>
            </article>
          );
        }}
        onRowClick={(row) => router.push(row.href)}
        search={{
          placeholder: "Filter containers",
          countLabel: { singular: "container", plural: "containers" },
          by: (row) => [
            row.name,
            row.reference,
            labels[row.kind],
            row.status,
            ...row.scopes.flatMap((scope) => [scope.project, scope.environment]),
          ],
        }}
        loadingRows={5}
        empty={
          project || environment || kind || status
            ? "No containers match these filters."
            : "No containers are running on this instance."
        }
      />
      <Dialog open={logs !== null} onOpenChange={(open) => !open && setLogs(null)}>
        <DialogContent className="w-[calc(100vw-2rem)] max-w-6xl sm:max-w-6xl">
          <DialogTitle className="flex items-center gap-2 pr-8 font-mono text-sm">
            <span className="sr-only">Logs</span>
            <FileTextIcon className="size-5 shrink-0" aria-hidden="true" />
            <span>{logs?.name}</span>
          </DialogTitle>
          {logs && <ContainerLogs path={logs.logsPath} title={null} tall servers={logs.servers} />}
        </DialogContent>
      </Dialog>
      <Dialog open={shell !== null} onOpenChange={(open) => !open && setShell(null)}>
        <DialogContent className="w-[calc(100vw-2rem)] max-w-6xl sm:max-w-6xl">
          <DialogTitle className="flex items-center gap-2 pr-8 font-mono text-sm">
            <span className="sr-only">Shell</span>
            <TerminalSquareIcon className="size-5 shrink-0" aria-hidden="true" />
            <span>{shell?.name}</span>
          </DialogTitle>
          {shell && <AppShell path={`/apps/${shell.id}`} servers={shell.servers} />}
        </DialogContent>
      </Dialog>
      <NewWorkloadAppDialog
        open={creating === "app"}
        onOpenChange={(open) => !open && setCreating(null)}
        onCreated={(reference) => {
          setCreating(null);
          void refresh();
          router.push(`/projects/${reference.split("/").slice(0, 2).join("/")}`);
        }}
      />
      <NewDatastoreDialog
        open={creating === "database"}
        onOpenChange={(open) => !open && setCreating(null)}
        onCreated={(created) => {
          setCreating(null);
          void refresh();
          router.push(`/databases/${created.name}`);
        }}
      />
      <ConfirmDialog
        open={stopping !== null}
        onOpenChange={(open) => !open && setStopping(null)}
        title="Stop this container?"
        confirmLabel="Stop"
        description={
          stopping?.kind === "app"
            ? "Stops every replica and keeps the app and its data. It stays stopped until you start it again."
            : stopping?.kind === "datastore"
              ? "Attached apps will lose their database connection. Data stays on disk."
              : "Attached apps will lose access to this store. Objects stay on disk."
        }
        onConfirm={async () => {
          if (stopping) await stop(stopping);
          setStopping(null);
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`Delete ${deleting ? labels[deleting.kind].toLowerCase() : "resource"}?`}
        holdToConfirm
        confirmLabel="Delete"
        description={
          deleting?.kind === "datastore"
            ? "This permanently deletes the database and its data."
            : deleting?.kind === "objectstore"
              ? "This permanently deletes the managed store and its objects."
              : "This deletes the app and its containers. App volume data is retained."
        }
        onConfirm={async () => {
          if (deleting) await remove(deleting);
          setDeleting(null);
        }}
      />
    </>
  );
}
