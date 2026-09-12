"use client";

import { usePathname } from "next/navigation";
import { useState } from "react";

const REASONS = ["spam", "malware", "abuse", "other"] as const;

export function ReportButton({
  subjectType,
  subjectId,
}: {
  subjectType: "template" | "comment";
  subjectId: number;
}) {
  const pathname = usePathname();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState<(typeof REASONS)[number]>("spam");
  const [note, setNote] = useState("");
  const [state, setState] = useState<"idle" | "pending" | "sent">("idle");
  const [error, setError] = useState<string | null>(null);

  async function submit() {
    setState("pending");
    setError(null);
    try {
      const response = await fetch("/api/v1/reports", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ subjectType, subjectId, reason, note: note || undefined }),
      });
      if (response.status === 401) {
        window.location.href = `/api/auth/github?next=${encodeURIComponent(pathname)}`;
        return;
      }
      const parsed = await response.json();
      if (!response.ok) throw new Error(parsed.error?.message ?? "the server refused that");
      setState("sent");
    } catch (err) {
      setError(err instanceof Error ? err.message : "the server refused that");
      setState("idle");
    }
  }

  if (state === "sent") {
    return <p className="label text-fd-muted-foreground">Reported</p>;
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="label text-fd-muted-foreground hover:text-red-400"
      >
        Report
      </button>
    );
  }

  return (
    <div className="hud-frame border border-fd-border p-3">
      <select
        value={reason}
        onChange={(event) => setReason(event.target.value as (typeof REASONS)[number])}
        className="border border-fd-border bg-fd-background px-2 py-1 text-fd-foreground text-sm"
      >
        {REASONS.map((r) => (
          <option key={r} value={r}>
            {r}
          </option>
        ))}
      </select>
      <textarea
        value={note}
        onChange={(event) => setNote(event.target.value)}
        placeholder="Say more, if it helps (optional)"
        rows={2}
        className="mt-2 w-full border border-fd-border bg-fd-background p-2 text-fd-foreground text-sm outline-none focus:border-primary"
      />
      <div className="mt-2 flex items-center gap-3">
        <button
          type="button"
          onClick={submit}
          disabled={state === "pending"}
          className="label border border-fd-border px-2 py-1 text-fd-muted-foreground hover:border-primary hover:text-primary disabled:opacity-50"
        >
          Send report
        </button>
        <button
          type="button"
          onClick={() => setOpen(false)}
          className="label text-fd-muted-foreground hover:text-fd-foreground"
        >
          Cancel
        </button>
        {error ? <p className="text-red-400 text-xs">{error}</p> : null}
      </div>
    </div>
  );
}
