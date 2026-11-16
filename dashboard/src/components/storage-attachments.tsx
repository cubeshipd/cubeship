"use client";

import { PlusIcon, Trash2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import { RowAction, RowActions } from "@/components/row-actions";
import { SectionHeader } from "@/components/section-header";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  type App,
  api,
  type Bucket,
  type ObjectStore,
  type ObjectStoreAttachment,
  objectStorePath,
} from "@/lib/api";
import { message } from "@/lib/errors";

// The apps wired to this store, and the buckets each is pointed at.
//
// An attachment carries a bucket because a store holds many and an app
// wants one — which is also why the same app may appear twice, with a
// prefix keeping the two sets of variables apart.
export function Attachments({ store, onChanged }: { store: ObjectStore; onChanged: () => void }) {
  const router = useRouter();
  const [attaching, setAttaching] = useState(false);
  const [detaching, setDetaching] = useState<ObjectStoreAttachment | null>(null);
  const [apps, setApps] = useState<App[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Loaded here rather than when the dialog opens: a select with
  // nothing in it is a disabled select, and a dialog whose first field
  // is disabled opens focused on whatever comes after it.
  useEffect(() => {
    api
      .get<App[]>("/apps")
      .then(setApps)
      .catch((e) => setError(message(e)));
  }, []);

  const columns: Column<ObjectStoreAttachment>[] = [
    {
      id: "app",
      header: "App",
      width: 38,
      sortBy: (a) => a.app,
      cell: (a) => <span className="font-mono text-sm">{a.app}</span>,
    },
    {
      id: "bucket",
      header: "Bucket",
      width: 24,
      sortBy: (a) => a.bucket,
      cell: (a) => <span className="font-mono text-xs">{a.bucket}</span>,
    },
    {
      id: "variables",
      header: "Variables",
      width: 28,
      // The names, never the values: one of them is the secret key.
      cell: (a) => (
        <span className="font-mono text-xs text-muted-foreground">
          {a.prefix ? `${a.prefix}S3_*` : "S3_*"}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      cell: (a) => (
        <RowActions>
          <RowAction
            icon={Trash2Icon}
            label={`Detach ${a.app}`}
            danger
            onClick={() => setDetaching(a)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <SectionHeader
        title="Attached apps"
        sub="Attached apps receive S3 connection variables on their next deploy."
        actions={
          <Button onClick={() => setAttaching(true)}>
            <PlusIcon />
            Attach an app
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={store.attachments}
        search={{
          placeholder: "Filter apps",
          countLabel: { singular: "app", plural: "apps" },
          by: (a) => [a.app, a.bucket, a.prefix],
        }}
        rowKey={(a) => `${a.app}:${a.bucket}`}
        onRowClick={(a) => router.push(`/projects/${a.app}`)}
        empty="Nothing is attached yet."
      />

      <AttachDialog
        store={store}
        apps={apps}
        open={attaching}
        onOpenChange={setAttaching}
        onAttached={onChanged}
      />

      {/* No word to type. Detaching is undone by attaching again, so
          the guard only has to stop the misclick. */}
      <ConfirmDialog
        open={detaching !== null}
        onOpenChange={(open) => !open && setDetaching(null)}
        title="Detach this app?"
        confirmLabel="Detach"
        description={
          <>
            <code className="text-foreground">{detaching?.app}</code> keeps the variables it is
            running with until it is deployed again, and comes up without them after that. This is
            not how you cut an app off in a hurry — rotating the key is.
          </>
        }
        onConfirm={async () => {
          if (!detaching) return;
          await api.del(
            `${objectStorePath(store.name)}/attachments/${detaching.app}?bucket=${encodeURIComponent(detaching.bucket)}`,
          );
          setDetaching(null);
          onChanged();
        }}
      />
    </>
  );
}

function AttachDialog({
  store,
  apps,
  open,
  onOpenChange,
  onAttached,
}: {
  store: ObjectStore;
  apps: App[] | null;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onAttached: () => void;
}) {
  const [app, setApp] = useState("");
  const [bucket, setBucket] = useState("");
  const [buckets, setBuckets] = useState<Bucket[]>([]);
  const [prefix, setPrefix] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setApp("");
    setPrefix("");
    setError(null);
    // The buckets the store can list, so picking one is a choice rather
    // than a name typed from memory. The API still takes a string —
    // a key scoped to one bucket may not be able to list them.
    api
      .get<Bucket[]>(`${objectStorePath(store.name)}/buckets`)
      .then((found) => {
        setBuckets(found);
        setBucket(found[0]?.name ?? "");
      })
      .catch(() => setBuckets([]));
  }, [open, store.name]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(`${objectStorePath(store.name)}/attachments`, { app, bucket, prefix });
      onAttached();
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>Attach an app</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <GlideSelect
              label="App"
              value={app}
              onChange={setApp}
              options={(apps ?? []).map((a) => ({ value: a.reference, label: a.reference }))}
              searchable
              className="w-full"
              triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
            />
            <GlideSelect
              label="Bucket"
              value={bucket}
              onChange={setBucket}
              options={buckets.map((b) => ({ value: b.name, label: b.name }))}
              searchable
              className="w-full"
              triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
            />
            <TextField
              label="Prefix"
              value={prefix}
              spellCheck={false}
              onChange={(e) => setPrefix(e.target.value.toUpperCase())}
              hint="Only when this app already has another bucket. Uppercase, ending in an underscore."
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!app || !bucket}>
              Attach
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
