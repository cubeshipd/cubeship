"use client";

import { PlusIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { DatastoreCard } from "@/components/datastore-card";
import { ErrorAlert } from "@/components/error-alert";
import { NewDatastoreDialog } from "@/components/new-datastore-dialog";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api, type Datastore } from "@/lib/api";
import { message } from "@/lib/errors";

// Every database this instance runs.
//
// Its own section rather than a shelf inside a project: a database
// belongs to the instance, and on one host the normal shape is a single
// Postgres serving several small apps that are routinely in different
// projects. Which apps use it is on the card, because a database
// nothing is attached to is one nothing can reach.
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

  return (
    <>
      <PageHeader
        title="Databases"
        sub="Postgres, MySQL and MariaDB, run by this instance for the apps on it. An app reaches one by being attached to it."
        actions={
          <Button onClick={() => setCreating(true)}>
            <PlusIcon />
            New database
          </Button>
        }
      />

      <ErrorAlert error={error} />

      {datastores?.length === 0 && (
        <Card>
          <CardContent className="flex items-center justify-between gap-4 py-2">
            <span className="text-sm text-muted-foreground">
              This instance runs no databases yet.
            </span>
            <Button variant="outline" onClick={() => setCreating(true)}>
              Create one
            </Button>
          </CardContent>
        </Card>
      )}

      {datastores && datastores.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {datastores.map((d) => (
            <DatastoreCard key={d.name} datastore={d} />
          ))}
        </div>
      )}

      <NewDatastoreDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(created) => router.push(`/databases/${created.name}`)}
      />
    </>
  );
}
