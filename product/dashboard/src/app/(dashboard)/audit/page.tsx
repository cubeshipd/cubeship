"use client";

import { cn } from "cn";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { type Column, DataTable } from "@/components/data-table";
import { type DateRange, DateRangePicker } from "@/components/date-range";
import { ErrorAlert } from "@/components/error-alert";
import { SearchBar } from "@/components/search-bar";
import { SearchableSelect } from "@/components/searchable-select";
import { useSession } from "@/components/session-context";
import { UserAvatar } from "@/components/user-avatar";
import { type AuditEvent, type AuditPage, api, type InstanceUser } from "@/lib/api";
import { message } from "@/lib/errors";

// Who changed what, and through which door.
//
// Recorded where requests come in — the API and MCP — so nothing a module
// adds later escapes it. A read is only here when it was refused: a key
// trying to read a secret is worth a row, a thousand listings are not.
//
// **Filtered by the daemon, not by the page.** The log is paged, and a
// filter over the loaded page would say "nothing" about an event one
// page further back.
//
// An admin's screen, like Users: it is everybody's activity.

const VIA = [
  { value: "", label: "Any channel" },
  { value: "dashboard", label: "Dashboard" },
  { value: "api", label: "API" },
  { value: "mcp", label: "MCP" },
];

const OUTCOMES = [
  { value: "", label: "Any outcome" },
  { value: "ok", label: "Done" },
  { value: "refused", label: "Refused" },
  { value: "failed", label: "Failed" },
];

const OUTCOME_LABEL: Record<AuditEvent["outcome"], string> = {
  ok: "Done",
  refused: "Refused",
  failed: "Failed",
};

export default function AuditLog() {
  const me = useSession();
  const [events, setEvents] = useState<AuditEvent[] | null>(null);
  const [next, setNext] = useState<number | undefined>();
  const [older, setOlder] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [people, setPeople] = useState<InstanceUser[]>([]);

  const [who, setWho] = useState("");
  const [via, setVia] = useState("");
  const [outcome, setOutcome] = useState("");
  const [range, setRange] = useState<DateRange>({});
  const [search, setSearch] = useState("");
  // Typed, then asked for: a request per keystroke is a race between them.
  const [target, setTarget] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setTarget(search.trim()), 300);
    return () => clearTimeout(t);
  }, [search]);

  const query = useMemo(() => {
    const q = new URLSearchParams();
    if (who) q.set("user", who);
    if (via) q.set("via", via);
    if (outcome) q.set("outcome", outcome);
    if (target) q.set("target", target);
    if (range.from) q.set("from", range.from.toISOString());
    if (range.to) q.set("to", range.to.toISOString());
    return q;
  }, [who, via, outcome, target, range]);

  const load = useCallback(
    async (before?: number) => {
      const q = new URLSearchParams(query);
      if (before) q.set("before", String(before));
      const s = q.toString();
      const page = await api.get<AuditPage>(s ? `/audit?${s}` : "/audit");
      setEvents((prev) => (before && prev ? [...prev, ...page.events] : page.events));
      setNext(page.next);
    },
    [query],
  );

  useEffect(() => {
    if (me.role !== "admin") return;
    setEvents(null);
    load().catch((e) => setError(message(e)));
  }, [load, me.role]);

  useEffect(() => {
    if (me.role !== "admin") return;
    api
      .get<{ users: InstanceUser[] }>("/users")
      .then((r) => setPeople(r.users))
      .catch(() => {});
  }, [me.role]);

  if (me.role !== "admin") {
    return <ErrorAlert error="The audit log is an admin's to read." />;
  }

  const filtered = who || via || outcome || target || range.from || range.to;

  const columns: Column<AuditEvent>[] = [
    {
      id: "at",
      header: "When",
      width: 14,
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
      width: 18,
      cell: (e) => {
        const person = people.find((p) => p.username === e.username);
        return (
          <span className="flex min-w-0 items-center gap-2.5">
            <UserAvatar
              person={person ?? { username: e.username }}
              className={cn("size-6", !person && "opacity-40 grayscale")}
            />
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-xs">{e.username}</span>
              <span className="truncate font-mono text-[11px] text-subtle-foreground">
                {e.key_name ? `${e.via} · ${e.key_name}` : e.via}
              </span>
            </span>
          </span>
        );
      },
    },
    {
      id: "what",
      header: "What",
      width: 44,
      wrap: true,
      cell: (e) => <span className="break-words text-sm">{e.summary}</span>,
    },
    {
      id: "outcome",
      header: "Outcome",
      width: 24,
      wrap: true,
      cell: (e) => (
        <span className="flex min-w-0 flex-col">
          <span
            className={cn(
              "text-xs",
              e.outcome === "ok" && "text-success",
              e.outcome === "refused" && "text-warning",
              e.outcome === "failed" && "text-destructive",
            )}
          >
            {OUTCOME_LABEL[e.outcome]}
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

      {/* The search takes its own row until there is room for it beside
          the four filters; squeezed in with them it showed three letters. */}
      <div className="mb-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-4 2xl:grid-cols-[minmax(0,1fr)_repeat(3,10rem)_13rem]">
        <SearchBar
          value={search}
          onChange={setSearch}
          placeholder="An app, a database, a project…"
          className="h-10 sm:col-span-2 lg:col-span-4 2xl:col-span-1"
        />
        <SearchableSelect
          value={who}
          onChange={setWho}
          choices={[
            { value: "", label: "Everyone" },
            ...people.map((p) => ({ value: p.username, label: p.username })),
          ]}
        />
        <SearchableSelect value={via} onChange={setVia} choices={VIA} />
        <SearchableSelect value={outcome} onChange={setOutcome} choices={OUTCOMES} />
        <DateRangePicker value={range} onChange={setRange} className="h-10 w-full" />
      </div>

      <DataTable
        columns={columns}
        rows={events}
        rowKey={(e) => String(e.id)}
        loadingRows={8}
        empty={
          filtered
            ? "Nothing matches these filters."
            : "Nothing yet. Every change made on this instance, and every refused attempt, lands here."
        }
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
