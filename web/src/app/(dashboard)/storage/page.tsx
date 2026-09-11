"use client";

import { PlusIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { NewObjectStoreDialog } from "@/components/new-object-store-dialog";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { api, type ObjectStore } from "@/lib/api";
import { message } from "@/lib/errors";

// Every object store this instance can reach, both kinds in one table.
//
// One table rather than two sections, because they are one thing to
// scan: what somebody comes here for is "where can this instance put a
// file", and splitting that by who runs the server would make you read
// both halves to answer it. Which kind each is has a column of its own,
// which is where the difference belongs — it is a fact about a row, not
// a reason for two tables.
export default function StoragePage() {
  const router = useRouter();
  const [stores, setStores] = useState<ObjectStore[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<ObjectStore[]>("/objectstores")
      .then(setStores)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  const columns: Column<ObjectStore>[] = [
    {
      id: "name",
      header: "Name",
      width: 26,
      sortBy: (s) => s.name,
      cell: (s) => <span className="font-mono text-sm">{s.name}</span>,
    },
    {
      id: "kind",
      header: "Kind",
      width: 16,
      sortBy: (s) => s.kind,
      // The one fact that changes what deleting a row means, so it is
      // in the column you glance at rather than a page deeper.
      cell: (s) => (
        <span className="text-sm">{s.kind === "managed" ? "On this host" : "Linked"}</span>
      ),
    },
    {
      id: "provider",
      header: "Where",
      width: 38,
      sortBy: (s) => s.provider_label,
      // Two lines, because one of them is an address. The provider is
      // what you scan for and the endpoint is what tells two of the
      // same provider apart — and side by side the endpoint pushed the
      // status column off its own edge.
      cell: (s) => (
        <span className="block min-w-0">
          <span className="block text-sm">{s.provider_label}</span>
          <span className="block truncate font-mono text-xs text-muted-foreground">
            {s.endpoint.replace(/^https?:\/\//, "")}
          </span>
        </span>
      ),
    },
    {
      id: "status",
      header: "Status",
      width: 20,
      sortBy: (s) => s.status,
      cell: (s) => (
        <span className="flex items-center gap-2">
          <StatusBadge value={s.status} />
          {/* A published port is the difference between something on a
              private network and something on the internet, which is
              worth a glance rather than a click. */}
          {s.exposed_port ? (
            <span className="font-mono text-xs text-warning">:{s.exposed_port}</span>
          ) : null}
        </span>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="Object storage"
        actions={
          <Button onClick={() => setAdding(true)}>
            <PlusIcon />
            Add storage
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={stores}
        rowKey={(s) => s.name}
        onRowClick={(s) => router.push(`/storage/${s.name}`)}
        empty={
          <span className="flex items-center justify-between gap-4">
            This instance reaches no object storage yet.
            <Button variant="outline" onClick={() => setAdding(true)}>
              Add one
            </Button>
          </span>
        }
      />

      <NewObjectStoreDialog
        open={adding}
        onOpenChange={setAdding}
        onCreated={(created) => router.push(`/storage/${created.name}`)}
      />
    </>
  );
}
