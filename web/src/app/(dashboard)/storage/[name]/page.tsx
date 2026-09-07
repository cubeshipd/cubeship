"use client";

import {
  ChevronLeftIcon,
  PlayIcon,
  PlusIcon,
  PowerIcon,
  SettingsIcon,
  Trash2Icon,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogs } from "@/components/container-logs";
import { CopyField } from "@/components/copy-field";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
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
  type ObjectStoreCredentials,
  objectStorePath,
} from "@/lib/api";
import { message } from "@/lib/errors";

// One object store, on one page: how to connect to it, and what is in
// it.
//
// Sections rather than tabs, like a database's page and for the same
// reason — neither is an alternative to the other, and hiding one
// behind a click made you click through both every time.
//
// The buckets come second. Connecting is what an app needs and is
// answered once; the buckets are what you came to open, and they are
// the thing at the bottom of the page you scroll to and then click
// into.
export default function ObjectStorePage({ params }: PageProps<"/storage/[name]">) {
  const { name } = use(params);
  return <Detail name={name} />;
}

function Detail({ name }: { name: string }) {
  const [store, setStore] = useState<ObjectStore | null>(null);
  const [error, setError] = useState<string | null>(null);

  const path = objectStorePath(name);
  const reload = useCallback(() => {
    api
      .get<ObjectStore>(path)
      .then(setStore)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // Provisioning is a container being pulled and started, detached from
  // the request that asked for it — so this is the one screen that has
  // to go back and look. It stops the moment it settles.
  useEffect(() => {
    if (store?.status !== "provisioning") return;
    const timer = setInterval(reload, 2000);
    return () => clearInterval(timer);
  }, [store?.status, reload]);

  if (error && !store) return <ErrorAlert error={error} />;

  return (
    <>
      <Link
        href="/storage"
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        storage
      </Link>

      {/* The title does not wait on the fetch: its name is in the URL,
          because we are here having asked for this store by name. */}
      <PageHeader
        title={<span className="font-mono text-lg tracking-normal normal-case">{name}</span>}
        actions={
          store && (
            <>
              {store.kind === "managed" && <PowerButton store={store} onChanged={reload} />}
              <Button
                variant="outline"
                nativeButton={false}
                render={
                  <Link href={`/storage/${store.name}/settings`}>
                    <SettingsIcon />
                    Settings
                  </Link>
                }
              />
            </>
          )
        }
      />

      <ErrorAlert error={error} />

      {!store && <LoadingList rows={5} />}

      {/* Why it did not come up is the tail of what MinIO printed, and
          it appears nowhere else — the container it came from has been
          removed. */}
      {store?.status === "failed" && store.error && (
        <Card className="mb-4 border-l-2 border-destructive/30 border-l-destructive">
          <CardContent>
            <div className="text-[11px] tracking-[0.12em] text-destructive uppercase">
              It did not start
            </div>
            <pre className="mt-2 overflow-x-auto font-mono text-xs whitespace-pre-wrap text-muted-foreground">
              {store.error}
            </pre>
          </CardContent>
        </Card>
      )}

      {store && (
        <>
          <Connection store={store} />
          <Buckets store={store} />
          {store.kind === "managed" && store.has_container && (
            <ContainerLogs
              path={path}
              sub="What the server itself has printed. The first place to look when it refuses a key or will not start."
            />
          )}
        </>
      )}
    </>
  );
}

// Everything needed to point something at this store, as fields you
// copy and cannot edit.
//
// The keys are their own request and an admin's. A member sees the
// address and nothing else, which is most of what is here — so the
// refusal is a note rather than an error.
function Connection({ store }: { store: ObjectStore }) {
  const [creds, setCreds] = useState<ObjectStoreCredentials | null>(null);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setCreds(null);
    setForbidden(false);
    api
      .get<ObjectStoreCredentials>(`${objectStorePath(store.name)}/credentials`)
      .then(setCreds)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(message(e));
      });
  }, [store.name]);

  return (
    <section className="mb-8">
      <SectionHeader
        title="Connecting"
        sub={
          store.kind === "managed"
            ? "The endpoint is this store's own container name, which apps on this instance resolve on the shared network — and nothing off this host can."
            : `${store.provider_label}, reached with the account this store authenticates as.`
        }
      />
      <ErrorAlert error={error} />

      <div className="grid gap-4 sm:grid-cols-2">
        <CopyField label="Endpoint" value={store.endpoint} />
        <CopyField label="Region" value={store.region} />
        {creds && (
          <>
            <CopyField label="Access key id" value={creds.access_key} />
            <CopyField label="Secret access key" value={creds.secret_key} masked />
          </>
        )}
        {store.external_endpoint && (
          <CopyField
            label="From outside this host"
            value={store.external_endpoint}
            hint="Published on a host port, with no TLS in front of it. A firewall rule is what makes that safe."
          />
        )}
      </div>

      {forbidden && (
        <p className="mt-3 text-xs text-subtle-foreground">The keys are an admin's to read.</p>
      )}
      {store.path_style && (
        <p className="mt-3 text-xs leading-relaxed text-subtle-foreground">
          This endpoint addresses buckets by path. Most clients need telling —{" "}
          <code>--endpoint-url</code> plus <code>--force-path-style</code> for the AWS CLI.
        </p>
      )}
    </section>
  );
}

// What is in the store. A table, and a row is a door.
function Buckets({ store }: { store: ObjectStore }) {
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
      <section className="mb-8">
        <SectionHeader title="Buckets" />
        <Notice>
          What is in a store is an admin's to read. Cubeship never lets a member read data — there
          is no way to see a row of a database from here either — and a bucket is data.
        </Notice>
      </section>
    );
  }

  return (
    <section className="mb-8">
      <SectionHeader
        title="Buckets"
        sub={
          store.bucket
            ? "This store is pinned to one bucket: its key reaches that one and may not list the others."
            : undefined
        }
        actions={
          store.bucket ? undefined : (
            <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
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
        confirmWord={deleting?.name}
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
    </section>
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

// Stop and start, for a store this instance runs.
//
// Stopping is not a small act — every app writing to it starts failing
// — so it asks, and says what it costs.
function PowerButton({ store, onChanged }: { store: ObjectStore; onChanged: () => void }) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const stopped = store.status === "stopped";

  async function run(action: "start" | "stop") {
    setBusy(true);
    try {
      await api.post(`${objectStorePath(store.name)}/${action}`);
      onChanged();
    } finally {
      setBusy(false);
    }
  }

  if (stopped || !store.has_container) {
    return (
      <ActionButton busy={busy} variant="outline" onClick={() => run("start")}>
        <PlayIcon />
        Start
      </ActionButton>
    );
  }
  return (
    <>
      <Button variant="outline" onClick={() => setConfirming(true)}>
        <PowerIcon />
        Stop
      </Button>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Stop this store?"
        confirmLabel="Stop"
        description="Every app reading or writing to it starts failing. The container is stopped rather than removed, so its log survives — which is usually what you want next."
        onConfirm={async () => {
          await run("stop");
          setConfirming(false);
        }}
      />
    </>
  );
}
