"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import type { ReportRow } from "@/lib/templates/social";

type Action = "dismiss" | "unlist" | "remove" | "block";

async function patch(path: string, body: unknown): Promise<void> {
  const response = await fetch(path, {
    method: "PATCH",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    const parsed = await response.json();
    throw new Error(parsed.error?.message ?? "the server refused that");
  }
}

// One row's buttons: dismiss (resolve, no action), unlist or remove the
// reported template, or block its author. Each posts to the admin API
// and refreshes the page's own server-rendered data — there is nothing
// client-side worth keeping in sync beyond that.
export function ReportRowActions({ row }: { row: ReportRow }) {
  const router = useRouter();
  const [pending, setPending] = useState<Action | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function run(action: Action, body: unknown) {
    setPending(action);
    setError(null);
    try {
      await patch(
        action === "block"
          ? `/api/v1/admin/users/${row.subject.authorLogin}`
          : `/api/v1/admin/reports/${row.id}`,
        body,
      );
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "the server refused that");
    } finally {
      setPending(null);
    }
  }

  const canUnlistOrRemove = row.subjectType === "template" && row.subject.slug;

  return (
    <div className="flex flex-col items-end gap-2 text-right">
      <div className="flex flex-wrap justify-end gap-2 text-xs">
        <button
          type="button"
          disabled={pending !== null}
          onClick={() => run("dismiss", { resolution: "dismissed, no action taken" })}
          className="label border border-fd-border px-2 py-1 text-fd-muted-foreground hover:border-primary hover:text-primary disabled:opacity-50"
        >
          Dismiss
        </button>
        {canUnlistOrRemove ? (
          <button
            type="button"
            disabled={pending !== null}
            onClick={() =>
              run("unlist", {
                resolution: "unlisted the template",
                action: "unlist",
                subjectType: row.subjectType,
                slug: row.subject.slug,
              })
            }
            className="label border border-fd-border px-2 py-1 text-fd-muted-foreground hover:border-primary hover:text-primary disabled:opacity-50"
          >
            Unlist
          </button>
        ) : null}
        <button
          type="button"
          disabled={pending !== null}
          onClick={() =>
            run("remove", {
              resolution: `removed the ${row.subjectType}`,
              action: "remove",
              subjectType: row.subjectType,
              subjectId: row.subjectId,
              slug: row.subject.slug ?? undefined,
            })
          }
          className="label border border-fd-border px-2 py-1 text-fd-muted-foreground hover:border-red-400 hover:text-red-400 disabled:opacity-50"
        >
          Remove
        </button>
        {row.subject.authorLogin ? (
          <button
            type="button"
            disabled={pending !== null}
            onClick={() => run("block", { blocked: true })}
            className="label border border-fd-border px-2 py-1 text-fd-muted-foreground hover:border-red-400 hover:text-red-400 disabled:opacity-50"
          >
            Block {row.subject.authorLogin}
          </button>
        ) : null}
      </div>
      {error ? <p className="text-red-400 text-xs">{error}</p> : null}
    </div>
  );
}
