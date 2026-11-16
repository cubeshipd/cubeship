"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  FileTextIcon,
  HardDriveIcon,
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
import NavigationLink from "@/components/navigation-link";
import { NewObjectStoreDialog } from "@/components/new-object-store-dialog";
import { ActionMenu, type MenuItem } from "@/components/resource-header";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
  api,
  type ContainerUsage,
  can,
  formatBytes,
  formatCPU,
  type ObjectStore,
  objectStorePath,
} from "@/lib/api";
import { PROVIDER_ICONS } from "@/lib/credentials";
import { containersQuery, storesQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";
import { useOpenOnArrival } from "@/lib/open-on-arrival";

const href = (store: ObjectStore) => `/storage/${encodeURIComponent(store.name)}`;
const isStopped = (store: ObjectStore) => store.status === "stopped" || store.status === "failed";
const kindLabel = (store: ObjectStore) => (store.kind === "managed" ? "Managed" : "Linked");

function StoreIcon({ store }: { store: ObjectStore }) {
  const Icon =
    store.kind === "managed" ? HardDriveIcon : (PROVIDER_ICONS[store.provider] ?? HardDriveIcon);
  return <Icon className="size-4 shrink-0" aria-hidden="true" />;
}

export default function StoragePage() {
  const me = useSession();
  const router = useRouter();
  const client = useQueryClient();
  const inventory = useQuery(storesQuery);
  const readings = useQuery({ ...containersQuery, enabled: can(me, "servers") });
  const [kind, setKind] = useState("");
  const [status, setStatus] = useState("");
  const [creating, setCreating] = useState(false);
  const [logs, setLogs] = useState<ObjectStore | null>(null);
  const [stopping, setStopping] = useState<ObjectStore | null>(null);
  const [deleting, setDeleting] = useState<ObjectStore | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const canManage = can(me, "storage", "manage");
  useOpenOnArrival("new", setCreating);

  const usage = useMemo(
    () =>
      new Map<string, ContainerUsage>(
        (readings.data ?? [])
          .filter((reading) => reading.kind === "objectstore")
          .map((reading) => [reading.name, reading]),
      ),
    [readings.data],
  );
  const rows = inventory.data;
  const kindChoices = [
    { value: "", label: "All types" },
    { value: "managed", label: "Managed" },
    { value: "external", label: "Linked" },
  ];
  const statusChoices = [
    { value: "", label: "All statuses" },
    ...[...new Set((rows ?? []).map((store) => store.status))]
      .sort()
      .map((value) => ({ value, label: value })),
  ];
  const filtered = rows?.filter(
    (store) => (!kind || store.kind === kind) && (!status || store.status === status),
  );

  async function refresh() {
    await Promise.all([
      client.invalidateQueries({ queryKey: ["objectstores"] }),
      client.invalidateQueries({ queryKey: ["instance-containers"] }),
    ]);
  }

  async function power(store: ObjectStore, action: "start" | "stop") {
    setActionError(null);
    try {
      await api.post(`${objectStorePath(store.name)}/${action}`);
      await refresh();
    } catch (error) {
      setActionError(message(error));
    }
  }

  function actions(store: ObjectStore) {
    const managed = canManage && store.kind === "managed";
    const canStop =
      managed && store.has_container && !isStopped(store) && store.status !== "provisioning";
    const items: MenuItem[] = [
      ...(managed && isStopped(store)
        ? [
            {
              id: "start",
              label: "Start",
              icon: <PlayIcon className="size-4" />,
              onSelect: () => void power(store, "start"),
            },
          ]
        : []),
      ...(canStop
        ? [
            {
              id: "stop",
              label: "Stop",
              icon: <PauseIcon className="size-4" />,
              onSelect: () => setStopping(store),
            },
          ]
        : []),
      ...(store.kind === "managed" && store.has_container
        ? [
            {
              id: "logs",
              label: "View logs",
              icon: <FileTextIcon className="size-4" />,
              onSelect: () => setLogs(store),
            },
          ]
        : []),
      {
        id: "settings",
        label: "Settings",
        icon: <SettingsIcon className="size-4" />,
        onSelect: () => router.push(`${href(store)}?tab=access`),
      },
      ...(canManage
        ? [
            {
              id: "delete",
              label: "Delete",
              icon: <Trash2Icon className="size-4" />,
              destructive: true,
              onSelect: () => setDeleting(store),
            },
          ]
        : []),
    ];
    return <ActionMenu label={`Actions for ${store.name}`} items={items} compact />;
  }

  const columns: Column<ObjectStore>[] = [
    {
      id: "name",
      header: "Storage",
      width: 26,
      sortBy: (store) => store.name,
      cell: (store) => (
        <span className="flex min-w-0 items-center gap-3">
          <StoreIcon store={store} />
          <span className="truncate font-mono text-sm">{store.name}</span>
        </span>
      ),
    },
    {
      id: "provider",
      header: "Provider",
      width: 20,
      sortBy: (store) => store.provider_label,
      cell: (store) => (
        <span className="text-muted-foreground">
          {store.provider_label}
          {store.version ? ` ${store.version}` : ""}
        </span>
      ),
    },
    {
      id: "kind",
      header: "Type",
      width: 10,
      sortBy: kindLabel,
      cell: (store) => <span className="text-muted-foreground">{kindLabel(store)}</span>,
    },
    {
      id: "status",
      header: "Status",
      width: 12,
      sortBy: (store) => store.status,
      cell: (store) => <StatusBadge value={store.status} />,
    },
    {
      id: "apps",
      header: "Apps",
      width: 8,
      sortBy: (store) => store.attachments.length,
      cell: (store) => (
        <span className="font-mono text-sm text-muted-foreground">{store.attachments.length}</span>
      ),
    },
    {
      id: "cpu",
      header: "CPU",
      width: 8,
      align: "right",
      sortBy: (store) => usage.get(store.name)?.cpu_percent ?? -1,
      cell: (store) => (
        <span className="font-mono text-sm">
          {usage.has(store.name) ? formatCPU(usage.get(store.name)?.cpu_percent ?? 0) : "—"}
        </span>
      ),
    },
    {
      id: "memory",
      header: "Memory",
      width: 10,
      align: "right",
      sortBy: (store) => usage.get(store.name)?.memory_bytes ?? -1,
      cell: (store) => (
        <span className="font-mono text-sm">
          {usage.has(store.name) ? formatBytes(usage.get(store.name)?.memory_bytes ?? 0) : "—"}
        </span>
      ),
    },
    { id: "actions", header: "", width: 6, align: "right", cell: actions },
  ];

  return (
    <>
      <SectionHeader
        page
        title="Object storage"
        sub="Managed and linked S3 storage for your apps and backups."
        actions={
          canManage && (
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              New object store
            </Button>
          )
        }
      />
      <ErrorAlert error={inventory.isError ? message(inventory.error) : actionError} />
      <FilterPanel>
        <GlideSelect
          value={kind}
          onChange={setKind}
          options={kindChoices}
          label="Type"
          ariaLabel="Filter by type"
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
        className="[&_[data-slot=table]]:min-w-[860px] [&_[data-slot=table]]:table-auto"
        columns={columns}
        rows={inventory.isPending ? null : filtered}
        rowKey={(store) => store.name}
        onRowClick={(store) => router.push(href(store))}
        mobileRow={(store) => (
          <article className="rounded-lg border border-border bg-background p-3">
            <div className="flex items-center gap-3">
              <StoreIcon store={store} />
              <NavigationLink
                href={href(store)}
                className="min-w-0 flex-1 truncate font-mono text-sm font-medium"
              >
                {store.name}
              </NavigationLink>
              <StatusBadge value={store.status} />
              {actions(store)}
            </div>
            <p className="mt-1 pl-7 text-xs text-muted-foreground">
              {store.provider_label} · {kindLabel(store)} · {store.attachments.length}{" "}
              {store.attachments.length === 1 ? "app" : "apps"}
            </p>
          </article>
        )}
        search={{
          placeholder: "Filter storage",
          countLabel: { singular: "store", plural: "stores" },
          by: (store) => [store.name, store.provider, store.provider_label, kindLabel(store)],
        }}
        loadingRows={4}
        empty={
          kind || status
            ? "No object stores match these filters."
            : canManage
              ? "No object storage yet. Connect an S3 bucket or run managed storage."
              : "No object storage yet."
        }
      />
      <NewObjectStoreDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(store) => {
          setCreating(false);
          void refresh();
          router.push(href(store));
        }}
      />
      {logs && (
        <ContainerLogsDialog
          open
          onOpenChange={(open) => !open && setLogs(null)}
          path={objectStorePath(logs.name)}
          name={logs.name}
          hasContainer={logs.has_container}
        />
      )}
      <ConfirmDialog
        open={stopping !== null}
        onOpenChange={(open) => !open && setStopping(null)}
        title="Stop this store?"
        confirmLabel="Stop"
        description="Apps that read or write to this store will lose access until it starts again."
        onConfirm={async () => {
          if (stopping) await power(stopping, "stop");
          setStopping(null);
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title="Delete this store?"
        confirmLabel="Delete storage"
        holdToConfirm
        description={
          deleting?.kind === "external" ? (
            <>
              This instance forgets the endpoint and account.{" "}
              <strong>The bucket and its objects stay untouched.</strong>
            </>
          ) : (
            <>
              Its container and <strong>every object on this host</strong> will be deleted. There is
              no backup.
            </>
          )
        }
        onConfirm={async () => {
          if (deleting) {
            await api.del(objectStorePath(deleting.name));
            await refresh();
          }
          setDeleting(null);
        }}
      />
    </>
  );
}
