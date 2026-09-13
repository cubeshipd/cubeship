"use client";

import { cn } from "cn";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { useSession } from "@/components/session-context";
import { type AuditEvent, type AuditPage, api } from "@/lib/api";
import { message } from "@/lib/errors";

// Who changed what, and through which door.
//
// Recorded where requests come in — the API and MCP — so nothing a module
// adds later escapes it. A read is only here when it was refused: a key
// trying to read a secret is worth a row, a thousand listings are not.
//
// An admin's screen, like Users: it is everybody's activity.
export default function AuditLog() {
  const me = useSession();
  const [events, setEvents] = useState<AuditEvent[] | null>(null);
  const [next, setNext] = useState<number | undefined>();
  const [older, setOlder] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async (before?: number) => {
    const page = await api.get<AuditPage>(before ? `/audit?before=${before}` : "/audit");
    setEvents((prev) => (before && prev ? [...prev, ...page.events] : page.events));
    setNext(page.next);
  }, []);

  useEffect(() => {
    if (me.role !== "admin") return;
    load().catch((e) => setError(message(e)));
  }, [load, me.role]);

  if (me.role !== "admin") {
    return <ErrorAlert error="The audit log is an admin's to read." />;
  }

  const columns: Column<AuditEvent>[] = [
    {
      id: "at",
      header: "When",
      width: 14,
      sortBy: (e) => e.at,
      cell: (e) => (
        <span className="flex flex-col font-mono text-[11px] text-muted-foreground">
          <span>{new Date(e.at).toLocaleDateString()}</span>
          <span className="text-subtle-foreground">{new Date(e.at).toLocaleTimeString()}</span>
        </span>
      ),
    },
    {
      id: "who",
      header: "Who",
      width: 16,
      sortBy: (e) => e.username,
      cell: (e) => (
        <span className="flex min-w-0 flex-col">
          <span className="truncate text-xs">{e.username}</span>
          <span className="truncate font-mono text-[11px] text-subtle-foreground">
            {e.key_name ? `${e.via} · ${e.key_name}` : e.via}
          </span>
        </span>
      ),
    },
    {
      id: "action",
      header: "Action",
      width: 44,
      wrap: true,
      sortBy: (e) => e.action,
      cell: (e) => (
        <span className="flex min-w-0 flex-col">
          <span className="break-all font-mono text-xs">{e.action}</span>
          {e.target && (
            <span className="break-all font-mono text-[11px] text-muted-foreground">
              {e.target}
            </span>
          )}
        </span>
      ),
    },
    {
      id: "outcome",
      header: "Outcome",
      width: 26,
      wrap: true,
      sortBy: (e) => e.outcome,
      cell: (e) => (
        <span className="flex min-w-0 flex-col">
          <span
            className={cn(
              "font-mono text-xs",
              e.outcome === "ok" && "text-success",
              e.outcome === "refused" && "text-warning",
              e.outcome === "failed" && "text-destructive",
            )}
          >
            {e.status ? `${e.outcome} · ${e.status}` : e.outcome}
          </span>
          {e.detail && (
            <span className="break-words text-[11px] text-muted-foreground">{e.detail}</span>
          )}
        </span>
      ),
    },
  ];

  return (
    <>
      <ErrorAlert error={error} />
      <DataTable
        columns={columns}
        rows={events}
        rowKey={(e) => String(e.id)}
        loadingRows={8}
        search={{
          placeholder: "Filter by person, key, action or target",
          by: (e) => [e.username, e.key_name ?? "", e.action, e.target ?? "", e.outcome, e.via],
        }}
        empty="Nothing yet. Every change made on this instance, and every refused attempt, lands here."
      />
      {next !== undefined && (
        <div className="mt-4 flex justify-center">
          <ActionButton
            variant="outline"
            size="sm"
            busy={older}
            onClick={async () => {
              setOlder(true);
              try {
                await load(next);
              } catch (e) {
                setError(message(e));
              }
              setOlder(false);
            }}
          >
            Older
          </ActionButton>
        </div>
      )}
    </>
  );
}
