"use client";

import { PlusIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { NewDatastoreDialog } from "@/components/new-datastore-dialog";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { api, type Datastore, datastoreLabel } from "@/lib/api";
import { message } from "@/lib/errors";

// Every database this instance runs.
//
// A table rather than cards, like the registries and the DNS accounts:
// what someone comes here to do is scan a column — which engine, is it
// up — and cards make you read each one whole to find the one line you
// were after.
//
// What is attached to each is not among them. It is a list inside a
// row, as long as the number of apps, and in a listing it is either
// truncated to uselessness or the widest thing on the screen. It
// belongs on the database's own page, where it is a table of its own.
export default function DatabasesPage() {
  const router = useRouter();
  const [datastores, setDatastores] = useState<Datastore[] | null>(null);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<Datastore[]>("/datastores")
      .then(setDatastores)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  const columns: Column<Datastore>[] = [
    {
      id: "name",
      header: "Name",
      width: 40,
      sortBy: (d) => d.name,
      cell: (d) => <span className="font-mono text-sm">{d.name}</span>,
    },
    {
      id: "engine",
      header: "Engine",
      width: 26,
      sortBy: (d) => `${d.engine} ${d.version}`,
      cell: (d) => (
        <span className="text-sm">
          {datastoreLabel(d.engine)} <span className="text-muted-foreground">{d.version}</span>
        </span>
      ),
    },
    {
      id: "status",
      header: "Status",
      width: 20,
      sortBy: (d) => d.status,
      cell: (d) => <StatusBadge value={d.status} />,
    },
    {
      id: "exposed",
      header: "Exposed",
      width: 14,
      align: "right",
      sortBy: (d) => d.exposed_port ?? 0,
      cell: (d) =>
        d.exposed_port ? (
          // The one fact about a database worth carrying in a column you
          // only glance at: the difference between something on a
          // private network and something on the internet.
          <span className="font-mono text-xs text-warning">{d.exposed_port}</span>
        ) : (
          <span className="text-xs text-subtle-foreground">—</span>
        ),
    },
  ];

  return (
    <>
      <PageHeader
        title="Databases"
        sub="Postgres, MySQL, MariaDB, Redis and MongoDB, run by this instance for the apps on it. An app reaches one by being attached to it."
        actions={
          <Button onClick={() => setCreating(true)}>
            <PlusIcon />
            New database
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={datastores}
        rowKey={(d) => d.name}
        onRowClick={(d) => router.push(`/databases/${d.name}`)}
        empty={
          <span className="flex items-center justify-between gap-4">
            This instance runs no databases yet.
            <Button variant="outline" onClick={() => setCreating(true)}>
              Create one
            </Button>
          </span>
        }
      />

      <NewDatastoreDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(created) => router.push(`/databases/${created.name}`)}
      />
    </>
  );
}
