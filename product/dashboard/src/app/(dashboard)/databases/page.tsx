"use client";

import { DatabaseIcon, PlusIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { type ComponentType, useCallback, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal } from "@/components/header-rail";
import { MariaDBIcon, MongoDBIcon, MySQLIcon, PostgreSQLIcon, RedisIcon } from "@/components/icons";
import { NewDatastoreDialog } from "@/components/new-datastore-dialog";
import { ResourceCard, ResourceGrid } from "@/components/resource-grid";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { UsageRings } from "@/components/usage-ring";
import { api, type Datastore, datastoreLabel } from "@/lib/api";
import { message } from "@/lib/errors";
import { useOpenOnArrival } from "@/lib/open-on-arrival";
import { usageShares, useContainerUsage } from "@/lib/usage";

const ENGINE_ICONS: Record<string, ComponentType<{ className?: string }>> = {
  postgres: PostgreSQLIcon,
  mysql: MySQLIcon,
  mariadb: MariaDBIcon,
  redis: RedisIcon,
  mongodb: MongoDBIcon,
};

// Every database this instance runs, as cards: the engine's mark is
// what you find one by, before its name.
//
// What is attached to each is not on the card. It is a list as long as
// the number of apps, and it belongs on the database's own page, where
// it is a table of its own.
export default function DatabasesPage() {
  const router = useRouter();
  const [datastores, setDatastores] = useState<Datastore[] | null>(null);
  const [creating, setCreating] = useState(false);
  useOpenOnArrival("new", setCreating);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<Datastore[]>("/datastores")
      .then(setDatastores)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);
  const { usage, machine } = useContainerUsage("datastore");

  return (
    <>
      <RailPortal>
        {
          <Button onClick={() => setCreating(true)}>
            <PlusIcon />
            New database
          </Button>
        }
      </RailPortal>
      <ErrorAlert error={error} />

      <ResourceGrid
        rows={datastores}
        search={{ placeholder: "Filter databases", by: (d) => [d.name, d.engine, d.version] }}
        rowKey={(d) => d.name}
        card={(d) => (
          <ResourceCard
            href={`/databases/${d.name}`}
            icon={ENGINE_ICONS[d.engine] ?? DatabaseIcon}
            name={d.name}
            detail={`${datastoreLabel(d.engine)} ${d.version}`}
            status={<StatusBadge value={d.status} />}
            usage={
              d.has_container && (
                <UsageRings
                  name={d.name}
                  shares={usageShares(usage?.get(d.name), d.limits, machine)}
                />
              )
            }
          />
        )}
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
