"use client";

import { RefreshCwIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { SectionHeader } from "@/components/page-header";
import { message } from "@/lib/errors";

// What a container has printed.
//
// Fetched with `fetch` rather than through `api`, because the daemon
// answers this as text/plain — it is the log, not a document about the
// log — and the shared client decodes JSON.
//
// One component for an app and a database both, the same way the
// monitoring section is one: the two endpoints differ in their address
// and in nothing else.
export function ContainerLogs({
  path,
  title = "Logs",
  sub,
}: {
  path: string;
  title?: string;
  sub?: string;
}) {
  const [text, setText] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const res = await fetch(`/api${path}/logs`, { credentials: "same-origin" });
      const body = (await res.text()).trim();
      if (!res.ok) throw new Error(body || res.statusText);
      setText(body);
      setError(null);
    } catch (e) {
      setError(message(e));
    }
    setBusy(false);
  }, [path]);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <>
      <SectionHeader
        title={title}
        sub={sub}
        actions={
          <ActionButton variant="outline" size="sm" busy={busy} onClick={load}>
            <RefreshCwIcon />
            Refresh
          </ActionButton>
        }
      />

      <ErrorAlert error={error} />

      {/* Not polled. A log is something you come to read and then read
          again when you have changed something; refreshing it under
          somebody mid-sentence is the one thing a log viewer must not
          do. The button is there for when they want it. */}
      <pre className="mb-4 max-h-[420px] overflow-auto border border-border bg-black p-3 font-mono text-xs break-all whitespace-pre-wrap text-success/90">
        {text || (busy ? "" : "Nothing in the log yet.")}
      </pre>
    </>
  );
}
