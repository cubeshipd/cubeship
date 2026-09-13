"use client";

import { HardDriveIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { VolumeBackups } from "@/components/backups";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { Notice } from "@/components/notice";
import { SectionHeader } from "@/components/section-header";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { type App, type AppVolume, api } from "@/lib/api";
import { message } from "@/lib/errors";

// An app's volumes: directories that outlive its container.
//
// Adding one is refused by the daemon unless the app is one copy on one
// machine, and the screen says why before anybody tries.
export function AppVolumes({
  app,
  volumes,
  onChanged,
}: {
  app: App;
  volumes: AppVolume[] | null;
  onChanged: () => void;
}) {
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [added, setAdded] = useState<string | null>(null);
  const [removing, setRemoving] = useState<AppVolume | null>(null);
  const [keepData, setKeepData] = useState(true);

  const base = `/apps/${app.reference}/volumes`;
  const cannotHold =
    (volumes?.length ?? 0) === 0 && (app.nodes.length > 1 || app.scale > 1 || app.autoscale.max > 0)
      ? "It runs as more than one copy, on more than one server or with autoscaling on. A volume's data is on one machine and two copies cannot share it, so put the app back to one copy on one server first."
      : null;

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setAdded(null);
    try {
      const v = await api.post<AppVolume>(base, { path });
      setAdded(v.path);
      setPath("");
      onChanged();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <>
      <SectionHeader
        title="Volumes"
        sub="Directories inside the container whose contents survive deploys and restarts — what a database, a queue or a search index keeps its files in. Everything else the app writes is gone on its next deploy."
      />
      <Card>
        <CardContent className="space-y-4">
          <ErrorAlert error={error} />
          {volumes === null && <LoadingList rows={2} />}
          {volumes !== null && volumes.length === 0 && (
            <p className="text-sm text-muted-foreground">No volumes.</p>
          )}
          {volumes !== null && volumes.length > 0 && (
            <ul className="divide-y divide-border border border-border">
              {volumes.map((v) => (
                <li key={v.id} className="flex items-center gap-3 px-3 py-2">
                  <HardDriveIcon className="size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate font-mono text-sm">{v.path}</span>
                  <span className="shrink-0 font-mono text-xs text-muted-foreground">{v.node}</span>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Remove the volume at ${v.path}`}
                    onClick={() => {
                      setKeepData(true);
                      setRemoving(v);
                    }}
                  >
                    <Trash2Icon />
                  </Button>
                </li>
              ))}
            </ul>
          )}

          {cannotHold ? (
            <Notice>{cannotHold}</Notice>
          ) : (
            <form onSubmit={add} className="flex items-end gap-3">
              <div className="min-w-0 flex-1">
                <TextField
                  label="Path inside the container"
                  placeholder="/var/lib/rabbitmq"
                  spellCheck={false}
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                />
              </div>
              <ActionButton type="submit" busy={busy} disabled={path.trim() === ""}>
                Add volume
              </ActionButton>
            </form>
          )}

          {added && (
            <Notice>
              Added at {added}. Deploy the app to mount it: the container running now has no volume.
            </Notice>
          )}
          {volumes !== null && volumes.length > 0 && (
            <Notice>
              With a volume this app runs as one copy on {volumes[0].node}, and each deploy stops
              the old container before the new one starts — two containers on one directory corrupt
              it — so the app is unavailable for a few seconds per deploy.
            </Notice>
          )}
        </CardContent>
      </Card>

      {(volumes ?? []).map((v) => (
        <section key={v.id} className="mt-8">
          <h3 className="mb-3 font-mono text-sm text-muted-foreground">{v.path}</h3>
          <VolumeBackups app={app.reference} volumeID={v.id} />
        </section>
      ))}

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title="Remove volume"
        description={`The volume at ${removing?.path ?? ""} is taken off the app. The container running now keeps it until the next deploy.`}
        confirmWord={keepData ? undefined : removing?.path}
        confirmLabel="Remove volume"
        onConfirm={async () => {
          if (!removing) return;
          await api.del(`${base}/${removing.id}${keepData ? "" : "?delete_data=true"}`);
          setRemoving(null);
          onChanged();
        }}
      >
        <Label className="flex items-center gap-2 text-sm">
          <Checkbox checked={keepData} onCheckedChange={(v) => setKeepData(Boolean(v))} />
          Keep the data
        </Label>
        {!keepData && (
          <Notice tone="warning">The data is deleted permanently. Its backups are kept.</Notice>
        )}
      </ConfirmDialog>
    </>
  );
}
