"use client";

import { BroomIcon, TerminalIcon } from "lucide-react";
import { use, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal } from "@/components/header-rail";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { api, type ServerStorage } from "@/lib/api";
import { message } from "@/lib/errors";

const bytes = (n: number) =>
  Number.isFinite(n) ? `${(n / 1024 / 1024 / 1024).toFixed(1)} GB` : "—";

export default function ServerStoragePage({ params }: PageProps<"/servers/[name]/storage">) {
  const { name } = use(params);
  const [storage, setStorage] = useState<ServerStorage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<number | null>(null);
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    api
      .get<ServerStorage>(`/nodes/${encodeURIComponent(name)}/storage`)
      .then(setStorage)
      .catch((e) => setError(message(e)));
  }, [name]);

  async function prune() {
    setBusy(true);
    setError(null);
    try {
      const result = await api.post<{ reclaimed_bytes: number }>(
        `/nodes/${encodeURIComponent(name)}/storage/prune`,
      );
      setDone(result.reclaimed_bytes);
    } catch (e) {
      setError(message(e));
    }
    setBusy(false);
  }

  return (
    <>
      <RailPortal>
        <Button
          variant="outline"
          nativeButton={false}
          render={
            <a href={`/servers/${encodeURIComponent(name)}/shell`}>
              <TerminalIcon />
              Shell
            </a>
          }
        />
      </RailPortal>
      <ErrorAlert error={error} />
      {done !== null && (
        <Notice>Docker reclaimed {bytes(done)}. Refresh this page to read the new usage.</Notice>
      )}
      <Notice tone="warning">
        This cleans Docker's regenerable data only. Volumes are listed in usage but are never
        removed by this action.
      </Notice>
      {!storage && !error && <StorageSkeleton />}
      {storage && (
        <>
          <div className="grid gap-4 sm:grid-cols-3">
            <Card>
              <CardContent>
                <div className="text-xs text-muted-foreground">Images not used</div>
                <div className="mt-2 font-mono text-2xl">{bytes(storage.images_bytes)}</div>
                <div className="mt-1 text-xs text-muted-foreground">{storage.images} images</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent>
                <div className="text-xs text-muted-foreground">Build cache</div>
                <div className="mt-2 font-mono text-2xl">{bytes(storage.build_cache_bytes)}</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent>
                <div className="text-xs text-muted-foreground">Stopped containers</div>
                <div className="mt-2 font-mono text-2xl">
                  {bytes(storage.stopped_containers_bytes)}
                </div>
                <div className="mt-1 text-xs text-muted-foreground">
                  {storage.stopped_containers} containers
                </div>
              </CardContent>
            </Card>
          </div>
          <Card className="mt-4">
            <CardContent className="flex items-center justify-between gap-4">
              <div>
                <div className="font-medium">Clean regenerable Docker data</div>
                <div className="mt-1 text-xs text-muted-foreground">
                  Images, stopped containers and build cache.
                </div>
              </div>
              <ActionButton variant="destructive" busy={busy} onClick={() => setConfirming(true)}>
                <BroomIcon />
                Clean
              </ActionButton>
            </CardContent>
          </Card>
          <ConfirmDialog
            open={confirming}
            onOpenChange={setConfirming}
            title="Clean Docker storage?"
            confirmWord="CLEAN"
            confirmLabel="Clean storage"
            description="This removes unused images, stopped containers and build cache on this server. Volumes are never removed."
            onConfirm={prune}
          />
        </>
      )}
    </>
  );
}

function StorageSkeleton() {
  return (
    <>
      <div className="grid gap-4 sm:grid-cols-3" role="status" aria-label="Loading Docker storage">
        {["images", "cache", "containers"].map((key) => (
          <Card key={key}>
            <CardContent className="space-y-3">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-8 w-28" />
              <Skeleton className="h-3 w-24" />
            </CardContent>
          </Card>
        ))}
      </div>
      <Card className="mt-4">
        <CardContent className="flex items-center justify-between gap-4">
          <div className="space-y-2">
            <Skeleton className="h-5 w-56" />
            <Skeleton className="h-4 w-72" />
          </div>
          <Skeleton className="h-10 w-24" />
        </CardContent>
      </Card>
    </>
  );
}
