"use client";

import { useQueries, useQuery } from "@tanstack/react-query";
import { FileTextIcon, ServerIcon, TerminalSquareIcon } from "lucide-react";
import { useState } from "react";
import { ContainerLogsDialog } from "@/components/container-logs";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { FilterPanel } from "@/components/filter-panel";
import { GLIDE_FIELD_TRIGGER_CLASS, GlideSelect } from "@/components/glide-select";
import { Notice } from "@/components/notice";
import { RowAction, RowActions } from "@/components/row-actions";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { TerminalPanel } from "@/components/terminal-panel";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { api, type ClusterServer, type ComponentInventory, type SystemComponent } from "@/lib/api";
import { machinesQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

type ComponentRow = {
  server: ClusterServer;
  component: SystemComponent;
  status: string;
  available: boolean;
};

export default function Components() {
  const me = useSession();
  return me.role === "admin" && !me.grants ? (
    <ComponentsContent />
  ) : (
    <Notice>Only administrators can read Cubeship's internal components and logs.</Notice>
  );
}

function ComponentsContent() {
  const [node, setNode] = useState("");
  const [logs, setLogs] = useState<ComponentRow | null>(null);
  const [shell, setShell] = useState<ComponentRow | null>(null);
  const machines = useQuery({ ...machinesQuery, refetchInterval: 5_000 });
  const servers = machines.data ?? [];
  const inventories = useQueries({
    queries: servers.map((server) => ({
      queryKey: ["components", server.name],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        api.get<ComponentInventory>(`/nodes/${encodeURIComponent(server.name)}/components`, {
          signal,
        }),
      enabled: server.status === "ready",
      staleTime: 10_000,
      refetchInterval: 5_000,
      retry: false,
    })),
  });
  const rows: ComponentRow[] = servers.flatMap((server, index) => {
    const inventory = inventories[index];
    const available = server.status === "ready" && !inventory.isError;
    return (inventory.data?.components ?? []).map((component) => ({
      server,
      component,
      available,
      status: !available
        ? server.status === "ready"
          ? "unavailable"
          : server.status
        : component.health === "unhealthy"
          ? "unhealthy"
          : component.status,
    }));
  });
  const pending =
    machines.isPending ||
    (rows.length === 0 &&
      servers.some((server, index) => server.status === "ready" && inventories[index].isPending));
  const disconnected = servers.filter((server) => server.status !== "ready");
  const columns: Column<ComponentRow>[] = [
    {
      id: "component",
      header: "Component",
      width: 22,
      sortBy: (row) => row.component.name,
      cell: (row) => <span className="text-sm font-medium">{row.component.name}</span>,
    },
    {
      id: "container",
      header: "Container",
      width: 24,
      cell: (row) => (
        <span className="font-mono text-xs text-muted-foreground">{row.component.container}</span>
      ),
    },
    {
      id: "node",
      header: "Node",
      width: 18,
      sortBy: (row) => row.server.name,
      cell: (row) => <span className="font-mono text-xs">{row.server.name}</span>,
    },
    {
      id: "status",
      header: "Status",
      width: 16,
      sortBy: (row) => row.status,
      cell: (row) => <StatusBadge value={row.status} />,
    },
    {
      id: "restarts",
      header: "Restarts",
      width: 10,
      align: "right",
      sortBy: (row) => row.component.restarts,
      cell: (row) => row.component.restarts || "—",
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      cell: (row) => (
        <RowActions>
          <RowAction
            icon={FileTextIcon}
            label={`View ${row.component.name} logs on ${row.server.name}`}
            disabled={!row.available || !row.component.logs_available}
            onClick={() => setLogs(row)}
          />
          <RowAction
            icon={TerminalSquareIcon}
            label={`Open ${row.component.name} shell on ${row.server.name}`}
            disabled={!row.available || row.component.status !== "running"}
            onClick={() => setShell(row)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <SectionHeader page title="Components" sub="Internal services across your nodes." />
      <ErrorAlert error={machines.error ? message(machines.error) : null} />
      {servers.map((server, index) => (
        <ErrorAlert
          key={server.name}
          error={
            inventories[index].error ? `${server.name}: ${message(inventories[index].error)}` : null
          }
        />
      ))}
      {disconnected.length > 0 && (
        <Notice tone="warning">
          {disconnected.map((server) => server.name).join(", ")} cannot refresh component data until
          connected.
        </Notice>
      )}
      {(rows.length > 0 || !machines.isError) && (
        <>
          <FilterPanel>
            <GlideSelect
              label="Node"
              value={node}
              onChange={setNode}
              options={[
                { value: "", label: "All nodes" },
                ...servers.map((server) => ({ value: server.name, label: server.name })),
              ]}
              startIcon={<ServerIcon className="size-4" />}
              searchable
              className="w-full"
              triggerClassName={GLIDE_FIELD_TRIGGER_CLASS}
            />
          </FilterPanel>
          <DataTable
            columns={columns}
            rows={pending ? null : rows}
            rowKey={(row) => `${row.server.name}:${row.component.id}`}
            search={{
              placeholder: "Filter components",
              countLabel: { singular: "component", plural: "components" },
              by: (row) => [
                row.component.name,
                row.component.container,
                row.server.name,
                row.status,
              ],
              filter: (row) => !node || row.server.name === node,
            }}
            empty={node ? "No components on this node." : "No components reported by these nodes."}
          />
        </>
      )}
      <ContainerLogsDialog
        open={logs !== null}
        onOpenChange={(open) => !open && setLogs(null)}
        path={
          logs
            ? `/nodes/${encodeURIComponent(logs.server.name)}/components/${logs.component.id}`
            : ""
        }
        name={logs?.component.name ?? ""}
        hasContainer={logs !== null}
      />
      <Dialog open={shell !== null} onOpenChange={(open) => !open && setShell(null)}>
        <DialogContent className="w-[calc(100vw-2rem)] max-w-6xl sm:max-w-6xl">
          <DialogTitle className="flex items-center gap-2 pr-8 font-mono text-sm">
            <TerminalSquareIcon className="size-5 shrink-0" aria-hidden="true" />
            <span>{shell?.component.name} shell</span>
          </DialogTitle>
          {shell && (
            <TerminalPanel
              path={`/nodes/${encodeURIComponent(shell.server.name)}/components/${shell.component.id}/shell`}
              password
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
