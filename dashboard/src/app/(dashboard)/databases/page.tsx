"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DatabaseIcon,
  FileTextIcon,
  PauseIcon,
  PlayIcon,
  PlusIcon,
  SettingsIcon,
  Trash2Icon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogsDialog } from "@/components/container-logs";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { FilterPanel } from "@/components/filter-panel";
import { GlideSelect } from "@/components/glide-select";
import { MariaDBIcon, MongoDBIcon, MySQLIcon, PostgreSQLIcon, RedisIcon } from "@/components/icons";
import NavigationLink from "@/components/navigation-link";
import { NewDatastoreDialog } from "@/components/new-datastore-dialog";
import { ActionMenu, type MenuItem } from "@/components/resource-header";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
  api,
  type ContainerUsage,
  can,
  DATASTORE_STOPPED,
  type Datastore,
  datastoreLabel,
  datastorePath,
  formatBytes,
  formatCPU,
} from "@/lib/api";
import { containersQuery, datastoresQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";
import { useOpenOnArrival } from "@/lib/open-on-arrival";

const ICONS = {
  postgres: PostgreSQLIcon,
  mysql: MySQLIcon,
  mariadb: MariaDBIcon,
  redis: RedisIcon,
  mongodb: MongoDBIcon,
} as const;

function EngineIcon({ engine }: { engine: string }) {
  const Icon = ICONS[engine as keyof typeof ICONS] ?? DatabaseIcon;
  return <Icon className="size-4 shrink-0" aria-hidden="true" />;
}

const isStopped = (db: Datastore) => db.status === DATASTORE_STOPPED || db.status === "failed";

export default function DatabasesPage() {
  const me = useSession();
  const router = useRouter();
  const client = useQueryClient();
  const inventory = useQuery(datastoresQuery);
  const readings = useQuery({ ...containersQuery, enabled: can(me, "servers") });
  const [engine, setEngine] = useState("");
  const [status, setStatus] = useState("");
  const [creating, setCreating] = useState(false);
  const [logs, setLogs] = useState<Datastore | null>(null);
  const [stopping, setStopping] = useState<Datastore | null>(null);
  const [deleting, setDeleting] = useState<Datastore | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const canManage = can(me, "databases", "manage");
  useOpenOnArrival("new", setCreating);

  const usage = useMemo(
    () =>
      new Map<string, ContainerUsage>(
        (readings.data ?? [])
          .filter((reading) => reading.kind === "datastore")
          .map((reading) => [reading.name, reading]),
      ),
    [readings.data],
  );
  const rows = inventory.data;
  const engineChoices = [
    { value: "", label: "All engines" },
    ...[...new Set((rows ?? []).map((db) => db.engine))]
      .sort()
      .map((value) => ({ value, label: datastoreLabel(value) })),
  ];
  const statusChoices = [
    { value: "", label: "All statuses" },
    ...[...new Set((rows ?? []).map((db) => db.status))]
      .sort()
      .map((value) => ({ value, label: value })),
  ];
  const filtered = rows?.filter(
    (db) => (!engine || db.engine === engine) && (!status || db.status === status),
  );

  async function refresh() {
    await Promise.all([
      client.invalidateQueries({ queryKey: ["datastores"] }),
      client.invalidateQueries({ queryKey: ["instance-containers"] }),
    ]);
  }

  async function power(db: Datastore, action: "start" | "stop") {
    setActionError(null);
    try {
      await api.post(`${datastorePath(db.name)}/${action}`);
      await refresh();
    } catch (error) {
      setActionError(message(error));
    }
  }

  function actions(db: Datastore) {
    const canStop = canManage && db.has_container && !isStopped(db) && db.status !== "provisioning";
    const items: MenuItem[] = [
      ...(canManage && isStopped(db)
        ? [
            {
              id: "start",
              label: "Start",
              icon: <PlayIcon className="size-4" />,
              onSelect: () => void power(db, "start"),
            },
          ]
        : []),
      ...(canStop
        ? [
            {
              id: "stop",
              label: "Stop",
              icon: <PauseIcon className="size-4" />,
              onSelect: () => setStopping(db),
            },
          ]
        : []),
      ...(db.has_container
        ? [
            {
              id: "logs",
              label: "View logs",
              icon: <FileTextIcon className="size-4" />,
              onSelect: () => setLogs(db),
            },
          ]
        : []),
      {
        id: "settings",
        label: "Settings",
        icon: <SettingsIcon className="size-4" />,
        onSelect: () => router.push(`/databases/${db.name}?tab=resources`),
      },
      ...(canManage
        ? [
            {
              id: "delete",
              label: "Delete",
              icon: <Trash2Icon className="size-4" />,
              destructive: true,
              onSelect: () => setDeleting(db),
            },
          ]
        : []),
    ];
    return <ActionMenu label={`Actions for ${db.name}`} items={items} compact />;
  }

  const columns: Column<Datastore>[] = [
    {
      id: "name",
      header: "Database",
      width: 28,
      sortBy: (db) => db.name,
      cell: (db) => (
        <span className="flex min-w-0 items-center gap-3">
          <EngineIcon engine={db.engine} />
          <span className="truncate font-mono text-sm">{db.name}</span>
        </span>
      ),
    },
    {
      id: "engine",
      header: "Engine",
      width: 18,
      sortBy: (db) => `${db.engine} ${db.version}`,
      cell: (db) => (
        <span className="text-muted-foreground">
          {datastoreLabel(db.engine)} {db.version}
        </span>
      ),
    },
    {
      id: "status",
      header: "Status",
      width: 12,
      sortBy: (db) => db.status,
      cell: (db) => <StatusBadge value={db.status} />,
    },
    {
      id: "apps",
      header: "Apps",
      width: 10,
      sortBy: (db) => db.attachments.length,
      cell: (db) => (
        <span className="font-mono text-sm text-muted-foreground">{db.attachments.length}</span>
      ),
    },
    {
      id: "cpu",
      header: "CPU",
      width: 10,
      align: "right",
      sortBy: (db) => usage.get(db.name)?.cpu_percent ?? -1,
      cell: (db) => (
        <span className="font-mono text-sm">
          {usage.has(db.name) ? formatCPU(usage.get(db.name)?.cpu_percent ?? 0) : "—"}
        </span>
      ),
    },
    {
      id: "memory",
      header: "Memory",
      width: 12,
      align: "right",
      sortBy: (db) => usage.get(db.name)?.memory_bytes ?? -1,
      cell: (db) => (
        <span className="font-mono text-sm">
          {usage.has(db.name) ? formatBytes(usage.get(db.name)?.memory_bytes ?? 0) : "—"}
        </span>
      ),
    },
    { id: "actions", header: "", width: 6, align: "right", cell: actions },
  ];

  return (
    <>
      <SectionHeader
        page
        title="Databases"
        sub="Managed databases your apps connect to."
        actions={
          canManage && (
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              New database
            </Button>
          )
        }
      />
      <ErrorAlert error={inventory.isError ? message(inventory.error) : actionError} />
      <FilterPanel>
        <GlideSelect
          value={engine}
          onChange={setEngine}
          options={engineChoices}
          label="Engine"
          ariaLabel="Filter by engine"
        />
        <GlideSelect
          value={status}
          onChange={setStatus}
          options={statusChoices}
          label="Status"
          ariaLabel="Filter by status"
        />
      </FilterPanel>
      <DataTable
        className="[&_[data-slot=table]]:min-w-[820px] [&_[data-slot=table]]:table-auto"
        columns={columns}
        rows={inventory.isPending ? null : filtered}
        rowKey={(db) => db.name}
        onRowClick={(db) => router.push(`/databases/${db.name}`)}
        mobileRow={(db) => (
          <article className="rounded-lg border border-border bg-background p-3">
            <div className="flex items-center gap-3">
              <EngineIcon engine={db.engine} />
              <NavigationLink
                href={`/databases/${db.name}`}
                className="min-w-0 flex-1 truncate font-mono text-sm font-medium"
              >
                {db.name}
              </NavigationLink>
              <StatusBadge value={db.status} />
              {actions(db)}
            </div>
            <p className="mt-1 pl-7 text-xs text-muted-foreground">
              {datastoreLabel(db.engine)} {db.version} · {db.attachments.length}{" "}
              {db.attachments.length === 1 ? "app" : "apps"}
              {usage.has(db.name) &&
                ` · ${formatCPU(usage.get(db.name)?.cpu_percent ?? 0)} · ${formatBytes(usage.get(db.name)?.memory_bytes ?? 0)}`}
            </p>
          </article>
        )}
        search={{
          placeholder: "Filter databases",
          countLabel: { singular: "database", plural: "databases" },
          by: (db) => [db.name, db.engine, datastoreLabel(db.engine), db.version, db.status],
        }}
        loadingRows={4}
        empty={
          engine || status
            ? "No databases match these filters."
            : canManage
              ? "No databases yet. Create one to give your apps a managed data store."
              : "No databases yet."
        }
      />
      <NewDatastoreDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(db) => {
          setCreating(false);
          void refresh();
          router.push(`/databases/${db.name}`);
        }}
      />
      {logs && (
        <ContainerLogsDialog
          open
          onOpenChange={(open) => !open && setLogs(null)}
          path={datastorePath(logs.name)}
          name={logs.name}
          hasContainer={logs.has_container}
        />
      )}
      <ConfirmDialog
        open={stopping !== null}
        onOpenChange={(open) => !open && setStopping(null)}
        title={`Stop ${stopping?.name}?`}
        confirmLabel="Stop it"
        description={
          stopping?.attachments.length
            ? "The data stays where it is, but attached apps keep running and start failing to connect."
            : "The container stops and its data stays where it is. No app is attached to it."
        }
        onConfirm={async () => {
          if (stopping) await power(stopping, "stop");
          setStopping(null);
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`Delete ${deleting?.name}?`}
        holdToConfirm
        confirmLabel="Delete database"
        description={
          deleting?.attachments.length
            ? "The container is stopped and the data on disk goes with it. Attached apps will come up without a connection string the next time they are deployed."
            : "The container is stopped and the data on disk goes with it. Nothing is kept."
        }
        onConfirm={async () => {
          if (deleting) {
            await api.del(datastorePath(deleting.name));
            await refresh();
          }
          setDeleting(null);
        }}
      />
    </>
  );
}
