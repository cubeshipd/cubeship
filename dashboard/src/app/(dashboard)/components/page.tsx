"use client";

import { useQuery } from "@tanstack/react-query";
import { cn } from "cn";
import { BoxesIcon, RefreshCwIcon, TerminalIcon } from "lucide-react";
import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ContainerLogs } from "@/components/container-logs";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal } from "@/components/header-rail";
import { MachineSelector, useMachineSelection } from "@/components/machine-selector";
import { Notice } from "@/components/notice";
import { PageLoading } from "@/components/page-loading";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { api, type ComponentInventory } from "@/lib/api";
import { message } from "@/lib/errors";

export default function Components() {
  const me = useSession();
  return me.role === "admin" && !me.grants ? (
    <ComponentsContent />
  ) : (
    <Notice>Only administrators can read Cubeship's internal components and logs.</Notice>
  );
}

function ComponentsContent() {
  const machine = useMachineSelection();
  const [chosen, setChosen] = useState("daemon");
  const inventory = useQuery({
    queryKey: ["components", machine.server],
    queryFn: ({ signal }) =>
      api.get<ComponentInventory>(`/nodes/${encodeURIComponent(machine.server)}/components`, {
        signal,
      }),
    staleTime: 10_000,
    refetchInterval: 30_000,
    retry: false,
  });
  const items = inventory.data?.components ?? [];
  const component = items.find((item) => item.id === chosen) ?? items[0];
  const offline = machine.selected && machine.selected.status !== "ready";
  const healthy = items.filter(
    (item) => item.status === "running" && item.health !== "unhealthy",
  ).length;

  return (
    <>
      <RailPortal>
        <MachineSelector selection={machine} />
      </RailPortal>
      <header className="dashboard-overview-intro">
        <div>
          <h1>Inside Cubeship.</h1>
          <p>The services behind your platform, one machine at a time.</p>
        </div>
        <BoxesIcon className="size-12 text-primary/60" strokeWidth={1} />
      </header>
      <SectionHeader
        title={machine.server}
        sub={`${machine.selected?.control_plane === false ? "Worker" : "Control plane"} services. Application containers, databases and buckets remain in Workspace.`}
        actions={
          <ActionButton
            variant="outline"
            size="sm"
            busy={inventory.isFetching}
            onClick={() => void inventory.refetch()}
          >
            <RefreshCwIcon />
            Refresh
          </ActionButton>
        }
      />
      <ErrorAlert error={machine.query.error ? message(machine.query.error) : null} />
      <ErrorAlert error={inventory.error ? message(inventory.error) : null} />
      {offline && (
        <Notice tone="warning">
          This machine is {machine.selected?.status}. Component state cannot be confirmed and logs
          are unavailable until it reconnects.
        </Notice>
      )}
      {inventory.isPending ? (
        <PageLoading kind="grid" />
      ) : items.length > 0 ? (
        <>
          <p role="status" className="mb-4 font-mono text-xs text-muted-foreground">
            {inventory.isError || offline
              ? "Last known inventory"
              : `${healthy} running · ${items.length} components`}{" "}
            · Updated {new Date(inventory.dataUpdatedAt).toLocaleTimeString()}
          </p>
          <div className="component-grid">
            {items.map((item) => (
              <button
                key={item.id}
                type="button"
                aria-pressed={item.id === component?.id}
                onClick={() => setChosen(item.id)}
                className={cn("component-card", item.id === component?.id && "is-selected")}
              >
                <div className="flex items-center justify-between gap-3">
                  <h3>{item.name}</h3>
                  <StatusBadge value={item.health === "unhealthy" ? "unhealthy" : item.status} />
                </div>
                <p>{item.description}</p>
                <code>{item.container}</code>
                <div className="component-card-footer">
                  <span>
                    {item.restarts
                      ? `${item.restarts} restarts`
                      : item.logs_available
                        ? "Logs available"
                        : "No container"}
                  </span>
                  <TerminalIcon className="size-4" />
                </div>
              </button>
            ))}
          </div>
          {component && (
            <section className="component-detail" aria-label={`${component.name} details`}>
              <div className="component-facts">
                <span>
                  <strong>Component</strong>
                  {component.name}
                </span>
                <span>
                  <strong>Image</strong>
                  <code>{component.image || "Not installed on this machine"}</code>
                </span>
                <span>
                  <strong>Started</strong>
                  {component.started_at && !component.started_at.startsWith("0001")
                    ? new Date(component.started_at).toLocaleString()
                    : "—"}
                </span>
                {component.health && (
                  <span>
                    <strong>Health check</strong>
                    {component.health}
                  </span>
                )}
              </div>
              {component.detail && <Notice>{component.detail}</Notice>}
              {component.logs_available && !offline ? (
                <ContainerLogs
                  key={`${machine.server}:${component.id}`}
                  path={`/nodes/${encodeURIComponent(machine.server)}/components/${component.id}`}
                  title={`${component.name} logs`}
                  sub={`stdout and stderr from ${machine.server}. Follow is optional; logs stay separated by component and machine.`}
                  tall
                />
              ) : (
                <Notice>
                  There is no container log available here. A service running outside Docker must be
                  inspected on its host.
                </Notice>
              )}
            </section>
          )}
        </>
      ) : (
        !inventory.isError && <Notice>No Cubeship components reported by this machine.</Notice>
      )}
    </>
  );
}
