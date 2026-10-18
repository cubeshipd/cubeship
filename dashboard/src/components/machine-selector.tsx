"use client";

import { useQuery } from "@tanstack/react-query";
import { ServerIcon } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { GLIDE_FIELD_TRIGGER_CLASS, GlideSelect } from "@/components/glide-select";
import { useSession } from "@/components/session-context";
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
    <GlideSelect
      ariaLabel="Machine"
      value={server}
      onChange={select}
      options={(
        query.data ?? [{ name: server, control_plane: server === "control-plane", status: "ready" }]
      ).map((machine) => ({
        value: machine.name,
        label: machine.name,
        tag:
          machine.status === "ready"
            ? machine.control_plane
              ? "Control plane"
              : "Worker"
            : machine.status,
      }))}
      startIcon={<ServerIcon className="size-4" />}
      searchable
      align="right"
      className="w-64 max-w-[50vw]"
      triggerClassName={`${GLIDE_FIELD_TRIGGER_CLASS} font-mono`}
    />
  );
}
