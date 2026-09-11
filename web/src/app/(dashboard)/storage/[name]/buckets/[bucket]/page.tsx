"use client";

import {
  DownloadIcon,
  FileIcon,
  FolderIcon,
  FolderPlusIcon,
  Trash2Icon,
  UploadIcon,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, use, useCallback, useEffect, useRef, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
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
  api,
  bucketPath,
  downloadURL,
  formatBytes,
  type ObjectFolder,
  type ObjectListing,
  type StoredObject,
  uploadObject,
} from "@/lib/api";
import { message } from "@/lib/errors";

// An entry in the listing: a folder or a file, in one table.
//
// One table rather than two, with folders sorted first — which is what
// every file browser has done for forty years, and what makes "go into
// the thing I came for" one glance instead of two.
type Entry =
  | { kind: "folder"; name: string; prefix: string }
  | { kind: "file"; name: string; object: StoredObject };

// Browsing one bucket.
//
// **The folder lives in the query string, not the path.** A key may be
// anything — spaces, dots, a hash, a hundred characters — and a URL
// path made of one would need every segment encoded and decoded by hand
// at every level. The query carries the whole prefix as one value,
// which is also exactly what the API takes.
//
// And a folder is a common prefix, which is the only kind S3 has: it
// exists while something is under it. That is why "delete this folder"
// deletes everything under it, and why an empty one is a zero-byte
// marker object rather than a thing in its own right.
export default function BucketPage({ params }: PageProps<"/storage/[name]/buckets/[bucket]">) {
  const { name, bucket } = use(params);
  return (
    <Suspense>
      <Browser store={name} bucket={decodeURIComponent(bucket)} />
    </Suspense>
  );
}

function Browser({ store, bucket }: { store: string; bucket: string }) {
  const router = useRouter();
  const search = useSearchParams();
  const prefix = search.get("prefix") ?? "";

  const [listing, setListing] = useState<ObjectListing | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const [creatingFolder, setCreatingFolder] = useState(false);
  const [deleting, setDeleting] = useState<Entry | null>(null);
  const filePicker = useRef<HTMLInputElement>(null);

  const path = bucketPath(store, bucket);
  const reload = useCallback(() => {
    setListing(null);
    api
      .get<ObjectListing>(`${path}/objects?prefix=${encodeURIComponent(prefix)}`)
      .then((found) => {
        setListing(found);
        setError(null);
      })
      .catch((e) => {
        setListing({ prefix, folders: [], objects: [] });
        setError(message(e));
      });
  }, [path, prefix]);
  useEffect(reload, [reload]);

  function go(next: string) {
    const query = next ? `?prefix=${encodeURIComponent(next)}` : "";
    router.push(`/storage/${store}/buckets/${encodeURIComponent(bucket)}${query}`);
  }

  // A plain navigation, not a link and not a fetch. The response is
  // Content-Disposition: attachment, so the browser saves it and leaves
  // this page exactly where it was — where an <a target="_blank"> would
  // leave a stray tab behind and a fetch would hold the whole file in
  // memory before writing it anywhere.
  function download(object: StoredObject) {
    window.location.assign(downloadURL(store, bucket, object.key));
  }

  async function upload(files: FileList | null) {
    if (!files || files.length === 0) return;
    setUploading(true);
    setError(null);
    try {
      // One at a time rather than all at once: a browser will happily
      // open six connections and the daemon streams each one straight
      // through to the store, so the only thing parallelism buys here
      // is six half-finished uploads when something goes wrong.
      for (const file of Array.from(files)) {
        await uploadObject(store, bucket, prefix, file);
      }
      reload();
    } catch (e) {
      setError(message(e));
    }
    setUploading(false);
    if (filePicker.current) filePicker.current.value = "";
  }

  const entries: Entry[] = [
    ...(listing?.folders ?? []).map(
      (f: ObjectFolder): Entry => ({ kind: "folder", name: f.name, prefix: f.prefix }),
    ),
    ...(listing?.objects ?? []).map((o): Entry => ({ kind: "file", name: o.name, object: o })),
  ];

  const columns: Column<Entry>[] = [
    {
      id: "name",
      header: "Name",
      width: 56,
      wrap: true,
      // Folders first whatever the sort, because a folder is where you
      // are going and a file is where you have arrived.
      sortBy: (e) => `${e.kind === "folder" ? 0 : 1}${e.name}`,
      cell: (e) => (
        <span className="flex items-center gap-2 font-mono text-sm">
          {e.kind === "folder" ? (
            <FolderIcon className="size-4 shrink-0 text-primary" />
          ) : (
            <FileIcon className="size-4 shrink-0 text-subtle-foreground" />
          )}
          {e.name}
        </span>
      ),
    },
    {
      id: "size",
      header: "Size",
      width: 14,
      align: "right",
      sortBy: (e) => (e.kind === "file" ? e.object.size : -1),
      cell: (e) =>
        e.kind === "file" ? (
          <span className="font-mono text-xs text-muted-foreground">
            {formatBytes(e.object.size)}
          </span>
        ) : (
          // A folder has no size that is cheap to know: it would be a
          // listing of everything under it, at every depth, on every
          // row of this table.
          <span className="text-xs text-subtle-foreground">—</span>
        ),
    },
    {
      id: "modified",
      header: "Modified",
      width: 18,
      sortBy: (e) => (e.kind === "file" ? e.object.modified_at : ""),
      cell: (e) =>
        e.kind === "file" ? (
          <span className="text-xs text-muted-foreground">
            {new Date(e.object.modified_at).toLocaleString()}
          </span>
        ) : (
          <span className="text-xs text-subtle-foreground">—</span>
        ),
    },
    {
      id: "actions",
      header: "",
      width: 12,
      align: "right",
      cell: (e) => (
        <RowActions>
          {e.kind === "file" && (
            <RowAction
              icon={DownloadIcon}
              label={`Download ${e.name}`}
              onClick={() => download(e.object)}
            />
          )}
          <RowAction
            icon={Trash2Icon}
            label={`Delete ${e.name}`}
            danger
            onClick={() => setDeleting(e)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      {" "}
      <PageHeader
        title={<Breadcrumbs bucket={bucket} prefix={prefix} onGo={go} />}
        actions={
          <>
            <Button variant="outline" onClick={() => setCreatingFolder(true)}>
              <FolderPlusIcon />
              New folder
            </Button>
            <ActionButton busy={uploading} onClick={() => filePicker.current?.click()}>
              <UploadIcon />
              Upload
            </ActionButton>
            <input
              ref={filePicker}
              type="file"
              multiple
              hidden
              onChange={(e) => upload(e.target.files)}
            />
          </>
        }
      />
      <ErrorAlert error={error} />
      <DataTable
        columns={columns}
        rows={listing === null ? null : entries}
        rowKey={(e) => (e.kind === "folder" ? e.prefix : e.object.key)}
        // Opening a row is opening the thing in it: a folder is
        // somewhere to go, a file is something to save. Leaving files
        // inert would give every one of them a pointer cursor and no
        // answer to the click it invites.
        onRowClick={(e) => (e.kind === "folder" ? go(e.prefix) : download(e.object))}
        empty={prefix ? "This folder is empty." : "This bucket is empty."}
      />
      {/* One page at a time, because a bucket has no size worth
          rendering all of. The cursor is the store's own, not an
          offset: pages of a listing that is being written to do not
          line up any other way. */}
      {listing?.cursor && (
        <div className="mt-4 flex justify-center">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              api
                .get<ObjectListing>(
                  `${path}/objects?prefix=${encodeURIComponent(prefix)}&cursor=${encodeURIComponent(listing.cursor ?? "")}`,
                )
                .then((next) =>
                  setListing({
                    ...next,
                    folders: [...listing.folders, ...next.folders],
                    objects: [...listing.objects, ...next.objects],
                  }),
                )
                .catch((e) => setError(message(e)));
            }}
          >
            Show more
          </Button>
        </div>
      )}
      <NewFolderDialog
        store={store}
        bucket={bucket}
        prefix={prefix}
        open={creatingFolder}
        onOpenChange={setCreatingFolder}
        onCreated={reload}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={deleting?.kind === "folder" ? "Delete this folder?" : "Delete this file?"}
        confirmLabel="Delete"
        confirmWord={deleting?.kind === "folder" ? deleting.name : undefined}
        description={
          deleting?.kind === "folder" ? (
            <>
              <code className="text-foreground">{deleting.name}</code> and{" "}
              <strong>everything under it, at every depth</strong>. A folder is not a thing of its
              own here — it is the files under a prefix — so there is nothing else to delete.
            </>
          ) : (
            <>
              <code className="text-foreground">{deleting?.name}</code> goes now. There is no
              version history and nothing to restore it from.
            </>
          )
        }
        onConfirm={async () => {
          if (!deleting) return;
          if (deleting.kind === "folder") {
            await api.del(`${path}/folders?prefix=${encodeURIComponent(deleting.prefix)}`);
          } else {
            await api.del(`${path}/objects?key=${encodeURIComponent(deleting.object.key)}`);
          }
          setDeleting(null);
          reload();
        }}
      />
    </>
  );
}

// Where you are, one segment at a time, each of them a way back.
//
// The bucket is the first crumb rather than a separate title: the path
// you are looking at starts at the bucket, and putting its name
// somewhere else would make the trail start in the middle.
function Breadcrumbs({
  bucket,
  prefix,
  onGo,
}: {
  bucket: string;
  prefix: string;
  onGo: (prefix: string) => void;
}) {
  const segments = prefix.split("/").filter(Boolean);
  return (
    <span className="flex flex-wrap items-center gap-1 font-mono text-lg tracking-normal normal-case">
      <button
        type="button"
        onClick={() => onGo("")}
        className="transition-colors hover:text-primary"
      >
        {bucket}
      </button>
      {segments.map((segment, i) => (
        // The path down to here, which is unique in a way the segment
        // alone is not: `a/b/a/` has two crumbs called "a".
        <span key={segments.slice(0, i + 1).join("/")} className="flex items-center gap-1">
          <span className="text-subtle-foreground">/</span>
          {i === segments.length - 1 ? (
            <span className="text-muted-foreground">{segment}</span>
          ) : (
            <button
              type="button"
              onClick={() => onGo(`${segments.slice(0, i + 1).join("/")}/`)}
              className="transition-colors hover:text-primary"
            >
              {segment}
            </button>
          )}
        </span>
      ))}
    </span>
  );
}

function NewFolderDialog({
  store,
  bucket,
  prefix,
  open,
  onOpenChange,
  onCreated,
}: {
  store: string;
  bucket: string;
  prefix: string;
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
      await api.post(`${bucketPath(store, bucket)}/folders`, { prefix, name });
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
            <DialogTitle>New folder</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <TextField
              label="Name"
              value={name}
              autoFocus
              spellCheck={false}
              onChange={(e) => setName(e.target.value)}
              hint="S3 has no folders: this writes a zero-byte marker so an empty one shows up in the listing. It disappears the moment the folder holds anything else."
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
