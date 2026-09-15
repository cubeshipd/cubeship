"use client";

import { useQuery } from "@tanstack/react-query";
import { ChevronDownIcon, ServerIcon } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useSession } from "@/components/session-context";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { can } from "@/lib/api";
import { machinesQuery } from "@/lib/dashboard-queries";

export function useMachineSelection() {
  const me = useSession();
  const params = useSearchParams();
  const server = params.get("server") || "control-plane";
  const query = useQuery({ ...machinesQuery, enabled: can(me, "servers") });
  const selected = query.data?.find((machine) => machine.name === server);
  function select(name: string) {
    const url = new URL(window.location.href);
    if (name === "control-plane") url.searchParams.delete("server");
    else url.searchParams.set("server", name);
    window.history.replaceState(null, "", `${url.pathname}${url.search}${url.hash}`);
  }
  return { server, selected, query, select };
}

export function MachineSelector({
  selection,
}: {
  selection: ReturnType<typeof useMachineSelection>;
}) {
  const { server, query, select } = selection;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button type="button" className="machine-selector" aria-label={`Machine: ${server}`} />
        }
      >
        <ServerIcon className="size-4 shrink-0 text-primary" />
        <span className="truncate">{server}</span>
        <ChevronDownIcon className="size-3 shrink-0 text-muted-foreground" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-64">
        {(query.data ?? [{ name: "control-plane", control_plane: true, status: "ready" }]).map(
          (machine) => (
            <DropdownMenuItem
              key={machine.name}
              onClick={() => select(machine.name)}
              className="justify-between gap-6"
            >
              <span className={server === machine.name ? "text-primary" : ""}>{machine.name}</span>
              <span className="text-[11px] text-muted-foreground">
                {machine.status === "ready"
                  ? machine.control_plane
                    ? "Control plane"
                    : "Worker"
                  : machine.status}
              </span>
            </DropdownMenuItem>
          ),
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
