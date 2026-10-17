"use client";

import { HardDriveIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { VolumeBackups } from "@/components/backups";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { RowAction, RowActions } from "@/components/row-actions";
import { SectionHeader } from "@/components/section-header";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { type App, type AppVolume, api } from "@/lib/api";
import { message } from "@/lib/errors";

export function AppVolumes({
  app,
  volumes,
  onChanged,
}: {
  app: App;
  volumes: AppVolume[] | null;
  onChanged: () => void;
}) {
  const [adding, setAdding] = useState(false);
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [added, setAdded] = useState<string | null>(null);
  const [removing, setRemoving] = useState<AppVolume | null>(null);
  const [keepData, setKeepData] = useState(true);

  const base = `/apps/${app.reference}/volumes`;
  const cannotHold =
    (volumes?.length ?? 0) === 0 && (app.nodes.length > 1 || app.scale > 1 || app.autoscale.max > 0)
      ? "Volumes need one app copy on one machine. Scale down and choose one server before adding one."
      : null;

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setAdded(null);
    try {
      const volume = await api.post<AppVolume>(base, { path });
      setAdded(volume.path);
      setPath("");
      setAdding(false);
      onChanged();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  const columns: Column<AppVolume>[] = [
    {
      id: "path",
      header: "Container path",
      width: 60,
      sortBy: (volume) => volume.path,
      cell: (volume) => <span className="font-mono text-xs">{volume.path}</span>,
    },
    {
      id: "server",
      header: "Server",
      width: 30,
      sortBy: (volume) => volume.node,
      cell: (volume) => <span className="font-mono text-xs">{volume.node}</span>,
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      cell: (volume) => (
        <RowActions>
          <RowAction
            icon={Trash2Icon}
            label={`Remove volume at ${volume.path}`}
            danger
            onClick={() => {
              setKeepData(true);
              setRemoving(volume);
            }}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <section>
        <SectionHeader
          title="Volumes"
          sub="Container paths that keep their data across deployments."
          actions={
            <Button disabled={cannotHold !== null} onClick={() => setAdding(true)}>
              <PlusIcon />
              Add volume
            </Button>
          }
        />
        <div className="space-y-4">
          {cannotHold && <Notice>{cannotHold}</Notice>}
          <DataTable
            columns={columns}
            rows={volumes}
            rowKey={(volume) => String(volume.id)}
            empty="No persistent volumes yet."
            mobileRow={(volume) => (
              <article className="rounded-lg border border-border bg-background p-3">
                <div className="flex items-center gap-2">
                  <HardDriveIcon
                    className="size-4 shrink-0 text-muted-foreground"
                    aria-hidden="true"
                  />
                  <span className="min-w-0 flex-1 truncate font-mono text-sm">{volume.path}</span>
                  {columns[2].cell(volume)}
                </div>
                <p className="mt-2 pl-6 font-mono text-xs text-muted-foreground">{volume.node}</p>
              </article>
            )}
          />

          {added && <Notice>Added {added}. Deploy the app to mount it.</Notice>}
          {(volumes?.length ?? 0) > 0 && (
            <Notice>
              This app is pinned to {volumes?.[0]?.node}. Deployments briefly interrupt it while the
              container is replaced.
            </Notice>
          )}
        </div>
      </section>

      {(volumes ?? []).map((volume) => (
        <section key={volume.id} className="mt-6 space-y-3">
          <h3 className="font-mono text-sm text-muted-foreground">{volume.path}</h3>
          <VolumeBackups app={app.reference} volumeID={volume.id} node={volume.node} />
        </section>
      ))}

      <Dialog
        open={adding}
        onOpenChange={(open) => {
          setAdding(open);
          if (open) setError(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <form onSubmit={add}>
            <DialogHeader>
              <DialogTitle>Add volume</DialogTitle>
              <DialogDescription>Choose a path inside the container to persist.</DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-5">
              <ErrorAlert error={error} />
              <TextField
                label="Container path"
                autoFocus
                placeholder="/path/to/data"
                spellCheck={false}
                value={path}
                onChange={(e) => setPath(e.target.value)}
                hint="The current container gets the volume after the next deploy."
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setAdding(false)}>
                Cancel
              </Button>
              <ActionButton type="submit" busy={busy} disabled={path.trim() === ""}>
                Add volume
              </ActionButton>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title="Remove volume"
        description={`The volume at ${removing?.path ?? ""} is detached on the next deploy.`}
        holdToConfirm={!keepData && removing !== null}
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
