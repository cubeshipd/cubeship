import type { Metadata } from "next";
import Link from "next/link";
import { requireAdmin } from "@/lib/auth/guard";
import { openReports } from "@/lib/templates/social";
import { ReportRowActions } from "./report-actions";

// Reads the session and the database, so this page can never be static.
export const dynamic = "force-dynamic";

export const metadata: Metadata = { title: "Reports" };

export default async function AdminReportsPage() {
  const admin = await requireAdmin();
  const rows = await openReports(admin);

  return (
    <div className="mx-auto max-w-5xl px-6 py-12">
      <p className="label text-primary">Admin</p>
      <h1 className="mt-2 font-semibold text-2xl text-fd-foreground tracking-tight">
        Open reports
      </h1>

      {rows.length === 0 ? (
        <p className="mt-8 text-fd-muted-foreground text-sm">Nothing open.</p>
      ) : (
        <div className="hud-frame mt-8 divide-y divide-fd-border border border-fd-border">
          {rows.map((row) => (
            <div key={row.id} className="grid gap-4 p-4 sm:grid-cols-[1fr_auto]">
              <div>
                <Link href={row.subject.href} className="text-fd-foreground hover:text-primary">
                  {row.subject.label}
                </Link>
                <p className="mt-1 text-fd-muted-foreground text-xs">
                  reported by {row.reporter.login} for {row.reason}
                  {row.note ? <span> — “{row.note}”</span> : null}
                </p>
              </div>
              <ReportRowActions row={row} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
