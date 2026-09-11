"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { BackupTable } from "@/components/backups";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { SectionHeader } from "@/components/section-header";
import { api, type Backup, type BackupCoverage } from "@/lib/api";
import { message } from "@/lib/errors";

// Is this instance's data safe, and what is not.
//
// **It was a list of every dump on the instance, and that is a log.**
// Nobody reads a log to find out whether they are covered, and the list
// could not answer the question anyway: it is built from the backups,
// so a database nobody has ever backed up — the row that matters most —
// appeared in it nowhere at all. On an instance where nothing was
// scheduled it showed an empty table and no hint that anything was
// wrong.
//
// So it is one row per **database**, from `/backups/coverage`, which is
// built from the databases. The dumps themselves live on each
// database's own Backups tab, which is also where you act on them —
// every row here links straight to it.
//
// In Platform rather than beside Databases: what is here is not about
// any one database, and the second half of it is about databases that
// no longer exist.
export default function BackupsPage() {
  const router = useRouter();
  const [rows, setRows] = useState<BackupCoverage[] | null>(null);
  const [orphans, setOrphans] = useState<Backup[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    api
      .get<BackupCoverage[]>("/backups/coverage")
      .then(setRows)
      .catch((e) => setError(message(e)));
    api
      .get<Backup[]>("/backups/orphans")
      .then(setOrphans)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(load, [load]);

  // **Worst first, and that is the whole layout decision.** A report
  // sorted by name makes you read every row to find the one that needs
  // you; sorted by how bad it is, the answer is the top of the table
  // and an instance with nothing wrong is one you can stop reading
  // after a glance. The columns are still sortable for anybody who
  // wants it the other way.
  const sorted = rows ? [...rows].sort((a, b) => rank(a) - rank(b)) : null;

  const columns: Column<BackupCoverage>[] = [
    {
      id: "database",
      header: "Database",
      width: 22,
      sortBy: (c) => c.database,
      cell: (c) => (
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate font-mono">{c.database}</span>
          <span className="shrink-0 text-subtle-foreground text-xs">{c.engine}</span>
        </span>
      ),
    },
    {
      id: "coverage",
      header: "Coverage",
      width: 20,
      sortBy: (c) => rank(c),
      cell: (c) => {
        const s = state(c);
        return <span className={`text-xs uppercase tracking-wide ${s.tone}`}>{s.label}</span>;
      },
    },
    {
      id: "last",
      header: "Last good backup",
      width: 24,
      sortBy: (c) => c.last_good?.started_at ?? "",
      cell: (c) =>
        c.last_good ? (
          <span title={c.last_good.started_at}>
            {new Date(c.last_good.started_at).toLocaleString()}
          </span>
        ) : (
          <span className="text-subtle-foreground">never</span>
        ),
    },
    {
      id: "where",
      header: "Where it goes",
      width: 22,
      cell: (c) => {
        // The schedule's destination, not the last dump's: what somebody
        // is checking is where tonight's will land. A database with no
        // schedule has no answer, which is itself the answer.
        if (!c.schedule) return <span className="text-subtle-foreground">—</span>;
        if (!c.schedule.store) return <span className="text-warning">this machine</span>;
        return (
          <span className="truncate font-mono text-xs">
            {c.schedule.store}/{c.schedule.bucket}
          </span>
        );
      },
    },
    {
      id: "schedule",
      header: "Every day at",
      width: 12,
      sortBy: (c) => c.schedule?.at ?? "",
      cell: (c) =>
        c.schedule ? (
          <span className="font-mono text-xs">{c.schedule.at}</span>
        ) : (
          <span className="text-subtle-foreground">—</span>
        ),
    },
  ];

  return (
    <>
      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={sorted}
        rowKey={(c) => c.database}
        search={{ placeholder: "Filter databases", by: (c) => [c.database, c.engine] }}
        // Straight to the tab that acts on it, rather than to the
        // database's Overview and a second click to find Backups.
        onRowClick={(c) => router.push(`/databases/${c.database}?tab=backups`)}
        empty="This instance runs no databases."
      />

      {orphans && orphans.length > 0 && (
        <div className="mt-8">
          <SectionHeader
            title="Backups of deleted databases"
            sub="Kept on purpose — deleting a database is exactly when its backups matter — and reachable nowhere else. They can be downloaded and deleted; restoring one would mean choosing which database to load it into, which this release does not do."
          />
          {/* Their own table rather than rows mixed into the one above.
              Nothing here can be restored, and a table where some rows
              can be and some cannot is one somebody reads wrong. */}
          <BackupTable rows={orphans} onChanged={load} showDatabase />
        </div>
      )}
    </>
  );
}

// The five states a database can be in, worst first. The order is the
// sort, so adding one is a decision about where it belongs rather than
// an entry that lands at the bottom by default.
function rank(c: BackupCoverage): number {
  if (!c.can_back_up) return 4;
  if (c.count === 0) return 0;
  if (c.failing) return 1;
  if (!c.protected) return 2;
  return 3;
}

function state(c: BackupCoverage): { label: string; tone: string } {
  // An engine this instance does not dump. A row rather than an
  // omission: a database missing from a coverage report reads as one
  // nobody checked.
  if (!c.can_back_up) return { label: "Not backed up here", tone: "text-subtle-foreground" };
  if (c.count === 0) return { label: "Never backed up", tone: "text-destructive" };
  // Was working and is not. Said even when there is a good dump behind
  // it, because that dump is getting older every night.
  if (c.failing) return { label: "Failing", tone: "text-destructive" };
  // A dump beside the database it came from survives somebody dropping
  // a table and nothing else — not the disk, not the box.
  if (!c.protected) return { label: "On this machine", tone: "text-warning" };
  return { label: "Protected", tone: "text-success" };
}
