"use client";

import { HardDriveIcon, PlusIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal } from "@/components/header-rail";
import { MinIOIcon } from "@/components/icons";
import { NewObjectStoreDialog } from "@/components/new-object-store-dialog";
import { ResourceCard, ResourceGrid } from "@/components/resource-grid";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { UsageRings } from "@/components/usage-ring";
import { api, type ObjectStore } from "@/lib/api";
import { PROVIDER_ICONS } from "@/lib/credentials";
import { message } from "@/lib/errors";
import { useOpenOnArrival } from "@/lib/open-on-arrival";
import { usageShares, useContainerUsage } from "@/lib/usage";

// Every object store this instance can reach, both kinds in one grid.
//
// One grid rather than two sections, because they are one thing to
// scan: "where can this instance put a file". Which kind each is is a
// fact about a card, not a reason for two lists.
export default function StoragePage() {
  const router = useRouter();
  const [stores, setStores] = useState<ObjectStore[] | null>(null);
  const [adding, setAdding] = useState(false);
  useOpenOnArrival("new", setAdding);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<ObjectStore[]>("/objectstores")
      .then(setStores)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);
  const { usage, machine } = useContainerUsage("objectstore");

  return (
    <>
      <RailPortal>
        {
          <Button onClick={() => setAdding(true)}>
            <PlusIcon />
            Add storage
          </Button>
        }
      </RailPortal>
      <ErrorAlert error={error} />

      <ResourceGrid
        rows={stores}
        search={{ placeholder: "Filter stores", by: (s) => [s.name, s.provider, s.kind] }}
        rowKey={(s) => s.name}
        card={(s) => (
          <ResourceCard
            href={`/storage/${s.name}`}
            icon={
              s.provider === "minio" ? MinIOIcon : (PROVIDER_ICONS[s.provider] ?? HardDriveIcon)
            }
            name={s.name}
            // The kind is the one fact that changes what deleting it
            // means, so it is on the card rather than a page deeper.
            detail={
              s.kind === "managed"
                ? `${s.provider_label} · on this host`
                : `${s.provider_label} · ${s.endpoint.replace(/^https?:\/\//, "")}`
            }
            status={
              <span className="flex items-center gap-2">
                <StatusBadge value={s.status} />
                {/* A published port is the difference between a private
                    network and the internet, worth a glance. */}
                {s.exposed_port ? (
                  <span className="font-mono text-xs text-warning">:{s.exposed_port}</span>
                ) : null}
              </span>
            }
            // A linked store is somebody else's server: nothing here to read.
            usage={
              s.has_container && (
                <UsageRings
                  name={s.name}
                  shares={usageShares(usage?.get(s.name), s.limits, machine)}
                />
              )
            }
          />
        )}
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
