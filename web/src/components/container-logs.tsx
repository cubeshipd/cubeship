"use client";

import { cn } from "cn";
import { RefreshCwIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { SectionHeader } from "@/components/page-header";
import { SearchBar } from "@/components/search-bar";
import { Button } from "@/components/ui/button";
import { message } from "@/lib/errors";

// What a container has printed.
//
// Fetched with `fetch` rather than through `api`, because the daemon
// answers this as text/plain — it is the log, not a document about the
// log — and the shared client decodes JSON.
//
// One component for an app, a database and an object store, the same way
// the monitoring section is one: the endpoints differ in their address
// and in nothing else.

// TAILS are how many lines to ask for. A log is read from its end, and
// how far back "the end" is depends on what you are looking for — the
// last thing it said, or the last time it said something.
const TAILS = [200, 1000, 5000] as const;

// FOLLOW_MS is how often a followed log is re-read. Slower than a
// metric because a log is read rather than glanced at.
const FOLLOW_MS = 3000;

export function ContainerLogs({
  path,
  title = "Logs",
  sub,
  // tall is for a log that is the whole of what a screen is showing,
  // rather than one section among several.
  tall = false,
}: {
  path: string;
  title?: string | null;
  sub?: string;
  tall?: boolean;
}) {
  const [text, setText] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tail, setTail] = useState<number>(TAILS[0]);
  const [following, setFollowing] = useState(false);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const res = await fetch(`/api${path}/logs?tail=${tail}`, { credentials: "same-origin" });
      const body = (await res.text()).trim();
      if (!res.ok) throw new Error(body || res.statusText);
      setText(body);
      setError(null);
    } catch (e) {
      setError(message(e));
    }
    setBusy(false);
  }, [path, tail]);

  useEffect(() => {
    load();
  }, [load]);

  // **Polled only when asked.** Re-reading a log under somebody
  // mid-sentence is the one thing a log viewer must not do, so this is
  // off until somebody presses Follow — at which point it is what they
  // asked for, and the view goes to the bottom with it.
  useEffect(() => {
    if (!following) return;
    const timer = setInterval(load, FOLLOW_MS);
    return () => clearInterval(timer);
  }, [following, load]);

  const toolbar = (
    <div className="flex items-center gap-2">
      {TAILS.map((n) => (
        <Button
          key={n}
          type="button"
          variant="ghost"
          size="xs"
          aria-pressed={n === tail}
          onClick={() => setTail(n)}
          className={cn("font-mono", n === tail && "bg-secondary text-foreground")}
        >
          {n}
        </Button>
      ))}
      <Button
        type="button"
        variant="ghost"
        size="xs"
        aria-pressed={following}
        onClick={() => setFollowing(!following)}
        className={cn(following && "bg-secondary text-foreground")}
      >
        Follow
      </Button>
      <ActionButton variant="outline" size="sm" busy={busy && !following} onClick={load}>
        <RefreshCwIcon />
        Refresh
      </ActionButton>
    </div>
  );

  return (
    <>
      {title ? <SectionHeader title={title} sub={sub} actions={toolbar} /> : null}

      <ErrorAlert error={error} />

      <LogView
        text={text}
        busy={busy}
        tall={tall}
        empty="Nothing in the log yet."
        follow={following}
        trailing={title ? null : toolbar}
      />
    </>
  );
}

// The log itself: a filter, and a black panel that opens at its end.
//
// Separate from the fetching above because two things want the same
// panel out of two different places — a container's log, which is text
// from an endpoint, and a deployment's build output, which is a field
// on a JSON document. What they share is what somebody does with a log:
// look for a word in it, and read the last thing it said.
export function LogView({
  text,
  busy = false,
  tall = false,
  empty = "Nothing here yet.",
  // follow keeps the panel at the bottom as new text arrives, rather
  // than only on the first answer.
  follow = false,
  // trailing goes beside the filter, for a caller with no header to put
  // its controls in.
  trailing,
}: {
  text: string | null;
  busy?: boolean;
  tall?: boolean;
  empty?: string;
  follow?: boolean;
  trailing?: React.ReactNode;
}) {
  const [filter, setFilter] = useState("");
  const view = useRef<HTMLPreElement>(null);

  const lines = useMemo(() => {
    if (!text) return [];
    const all = text.split("\n");
    if (!filter.trim()) return all;
    const needle = filter.toLowerCase();
    return all.filter((line) => line.toLowerCase().includes(needle));
  }, [text, filter]);

  // A log is read from the end. Going there on the first answer saves
  // the scroll everybody does anyway, and going there on every answer
  // while following is what following means.
  const atEnd = useRef(false);
  useEffect(() => {
    if (text === null || !view.current) return;
    if (follow || !atEnd.current) {
      view.current.scrollTop = view.current.scrollHeight;
      atEnd.current = true;
    }
  }, [text, follow]);

  return (
    <div className="mb-4 space-y-2">
      {/* The filter is on its own line and above the log, not beside
          the buttons: at five thousand lines it is the control that
          gets used, and a field the width of a button is a field nobody
          types a word into. */}
      <div className="flex items-center gap-2">
        <SearchBar
          value={filter}
          onChange={setFilter}
          placeholder="Filter lines"
          className="min-w-0 flex-1"
          trailing={
            filter.trim() ? (
              <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
                {lines.length}
              </span>
            ) : undefined
          }
        />
        {trailing}
      </div>

      <pre
        ref={view}
        className={cn(
          "overflow-auto border border-border bg-black p-3 font-mono text-xs break-all whitespace-pre-wrap text-success/90",
          tall ? "h-[60vh]" : "max-h-[420px]",
        )}
      >
        {lines.length > 0
          ? lines.join("\n")
          : busy
            ? ""
            : filter.trim()
              ? "No line matches."
              : empty}
      </pre>
    </div>
  );
}
