"use client";
import { PlusIcon, Trash2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
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
  ApiError,
  api,
  type Bucket,
  bucketPath,
  type ObjectStore,
  objectStorePath,
} from "@/lib/api";
import { message } from "@/lib/errors";

// What is in the store. A table, and a row is a door.
export function Buckets({ store }: { store: ObjectStore }) {
  const router = useRouter();
  const [buckets, setBuckets] = useState<Bucket[] | null>(null);
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<Bucket | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [forbidden, setForbidden] = useState(false);

  const reload = useCallback(() => {
    api
      .get<Bucket[]>(`${objectStorePath(store.name)}/buckets`)
      .then((found) => {
        setBuckets(found);
        setError(null);
      })
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403 && !String(e.message).includes("refused")) {
          setForbidden(true);
          setBuckets([]);
          return;
        }
        setBuckets([]);
        setError(message(e));
      });
  }, [store.name]);
  useEffect(reload, [reload]);

  const columns: Column<Bucket>[] = [
    {
      id: "name",
      header: "Bucket",
      width: 70,
      sortBy: (b) => b.name,
      cell: (b) => <span className="font-mono text-sm">{b.name}</span>,
    },
    {
      id: "created",
      header: "Created",
      width: 20,
      sortBy: (b) => b.created_at ?? "",
      cell: (b) => (
        <span className="text-xs text-muted-foreground">
          {b.created_at ? new Date(b.created_at).toLocaleDateString() : "—"}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      cell: (b) => (
        <RowActions>
          <RowAction
            icon={Trash2Icon}
            label={`Delete ${b.name}`}
            danger
            onClick={() => setDeleting(b)}
          />
        </RowActions>
      ),
    },
  ];

  if (forbidden) {
    return (
      <>
        <SectionHeader title="Buckets" />
        <Notice>
          What is in a store is an admin's to read. Cubeship never lets a member read data — there
          is no way to see a row of a database from here either — and a bucket is data.
        </Notice>
      </>
    );
  }

  return (
    <>
      <SectionHeader
        title="Buckets"
        sub={
          store.bucket
            ? "This store is pinned to one bucket: its key reaches that one and may not list the others."
            : undefined
        }
        actions={
          store.bucket ? undefined : (
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              New bucket
            </Button>
          )
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={buckets}
        search={{
          placeholder: "Filter buckets",
          countLabel: { singular: "bucket", plural: "buckets" },
          by: (b) => [b.name],
        }}
        rowKey={(b) => b.name}
        onRowClick={(b) =>
          router.push(`/storage/${store.name}/buckets/${encodeURIComponent(b.name)}`)
        }
        empty="No buckets here yet."
      />

      <NewBucketDialog
        store={store}
        open={creating}
        onOpenChange={setCreating}
        onCreated={reload}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title="Delete this bucket?"
        confirmLabel="Delete"
        holdToConfirm
        description={
          <>
            <code className="text-foreground">{deleting?.name}</code> has to be empty first — the
            store refuses otherwise, and that refusal is worth keeping: a bucket delete that quietly
            took a thousand files with it is the one mistake here nobody recovers from.
          </>
        }
        onConfirm={async () => {
          if (!deleting) return;
          await api.del(bucketPath(store.name, deleting.name));
          setDeleting(null);
          reload();
        }}
      />
    </>
  );
}

function NewBucketDialog({
  store,
  open,
  onOpenChange,
  onCreated,
}: {
  store: ObjectStore;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setName("");
    setError(null);
  }, [open]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(`${objectStorePath(store.name)}/buckets`, { name });
      onCreated();
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
            <DialogTitle>New bucket</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <TextField
              label="Name"
              value={name}
              autoFocus
              spellCheck={false}
              onChange={(e) => setName(e.target.value.toLowerCase())}
              hint="Lowercase letters, digits, dots and dashes, 3 to 63 characters. On most providers it becomes part of a hostname, which is why the rules are that narrow."
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!name}>
              Create
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
