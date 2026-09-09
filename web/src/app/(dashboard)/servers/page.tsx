"use client";

import { PlusIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CopyField } from "@/components/copy-field";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { PageHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
import { SlugField } from "@/components/slug-field";
import { StatusBadge } from "@/components/status-badge";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  api,
  type ClusterServer,
  type ClusterServerCreated,
  formatBytes,
  formatCPU,
  type Settings,
} from "@/lib/api";
import { message } from "@/lib/errors";

// How often the cluster is re-read. Matched to the agents' own cadence:
// asking faster than the machines call in is two requests for one
// answer.
const REFRESH_MS = 10_000;

// The machines this instance is made of.
//
// The control plane is in the table rather than beside it. It is a
// machine like the others — the same daemon, the same numbers — and it
// differs in one thing that the list already says: it is the one that
// decides. A screen listing only "the other servers" would be a screen
// that cannot answer where something runs.
export default function Servers() {
  const [servers, setServers] = useState<ClusterServer[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<ClusterServer | null>(null);

  const reload = useCallback(() => {
    api
      .get<ClusterServer[]>("/nodes")
      .then((found) => {
        setServers(found);
        setError(null);
      })
      .catch((e) => setError(message(e)));
  }, []);

  useEffect(() => {
    reload();
    const timer = setInterval(reload, REFRESH_MS);
    return () => clearInterval(timer);
  }, [reload]);

  const columns: Column<ClusterServer>[] = [
    {
      id: "name",
      header: "Server",
      width: 22,
      sortBy: (s) => s.name,
      // No badge saying which one is the control plane: it is the row
      // called `control-plane`, it is always first, and it is the only
      // one with nothing to remove. Three things already say it.
      cell: (s) => <span className="truncate font-mono text-sm">{s.name}</span>,
    },
    {
      id: "status",
      header: "Status",
      width: 14,
      sortBy: (s) => s.status,
      cell: (s) => <StatusBadge value={s.status} />,
    },
    {
      id: "network",
      header: "Network",
      width: 12,
      sortBy: (s) => (s.in_mesh ? 1 : 0),
      // Two different questions, side by side on purpose. A machine
      // can be answering and not be on the cluster's network — its
      // agent calls in, and its containers cannot reach anything.
      cell: (s) =>
        s.in_mesh ? (
          <span className="font-mono text-xs text-success">mesh</span>
        ) : (
          <span
            className="font-mono text-xs text-subtle-foreground"
            title="This machine is not on the cluster's private network, so containers here cannot reach containers on the others."
          >
            alone
          </span>
        ),
    },
    {
      id: "machine",
      header: "Machine",
      width: 22,
      sortBy: (s) => s.cores,
      cell: (s) =>
        s.cores > 0 ? (
          <span className="font-mono text-xs text-muted-foreground">
            {s.cores} cores · {formatBytes(s.memory_total_bytes)} ·{" "}
            {formatBytes(s.disk_total_bytes)}
          </span>
        ) : (
          <span className="text-xs text-subtle-foreground">—</span>
        ),
    },
    {
      id: "load",
      header: "Now",
      width: 16,
      sortBy: (s) => s.cpu_percent ?? -1,
      cell: (s) =>
        s.cpu_percent === undefined ? (
          <span className="text-xs text-subtle-foreground">—</span>
        ) : (
          <span className="font-mono text-xs text-muted-foreground">
            {formatCPU(s.cpu_percent)} ·{" "}
            {s.memory_bytes === undefined ? "—" : formatBytes(s.memory_bytes)}
          </span>
        ),
    },
    {
      id: "containers",
      header: "Containers",
      width: 10,
      align: "right",
      sortBy: (s) => s.containers,
      cell: (s) => <span className="font-mono text-sm">{s.containers}</span>,
    },
    {
      id: "actions",
      header: "",
      width: 4,
      align: "right",
      // The control plane is not a machine this instance joined, so
      // there is nothing to remove it from. A missing button explains
      // nothing, but neither does a disabled one on a row whose whole
      // label already says why.
      cell: (s) =>
        s.control_plane ? null : (
          <RowActions>
            <RowAction
              icon={Trash2Icon}
              label={`Remove ${s.name}`}
              danger
              onClick={() => setRemoving(s)}
            />
          </RowActions>
        ),
    },
  ];

  return (
    <>
      <PageHeader
        title="Servers"
        actions={
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            <PlusIcon />
            Add a server
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={servers}
        rowKey={(s) => s.name}
        loadingRows={2}
        empty="No servers yet."
        className="mb-4"
      />

      <AddDialog open={adding} onOpenChange={setAdding} onAdded={reload} />

      {/* No word to type. Removing a server is a local act — the row
          goes and its credential with it, and nothing on that machine
          is touched — so the guard only has to stop the misclick. */}
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title="Remove this server?"
        confirmLabel="Remove"
        description={
          <>
            <code className="text-foreground">{removing?.name}</code> stops being part of this
            cluster and its credential stops working, so its agent is refused on its next call.{" "}
            <strong>Nothing on that machine is touched</strong> — whatever is running there goes on
            running until somebody stops it.
          </>
        }
        onConfirm={async () => {
          if (!removing) return;
          await api.del(`/nodes/${removing.name}`);
          setRemoving(null);
          reload();
        }}
      />
    </>
  );
}

// Adding a machine is two steps that do not touch each other: this
// mints a credential, and somebody runs one command on the box.
//
// So the dialog has two states rather than closing on success. The
// command carries the credential, it is the only time it exists in a
// form anybody can read, and closing the dialog on somebody before they
// have copied it would mean removing the server and adding it back.
function AddDialog({
  open,
  onOpenChange,
  onAdded,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onAdded: () => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [created, setCreated] = useState<ClusterServerCreated | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [settings, setSettings] = useState<Settings | null>(null);

  useEffect(() => {
    if (!open) return;
    setName("");
    setDescription("");
    setCreated(null);
    setError(null);
    api
      .get<Settings>("/settings")
      .then(setSettings)
      .catch(() => setSettings(null));
  }, [open]);

  // Where the new machine will dial. The instance's domain when it has
  // one, and otherwise the address this dashboard was opened at — which
  // is by construction an address that reaches this box, and is what a
  // fresh install is reached at before there is a domain.
  const controlPlane = settings?.domain
    ? `https://${settings.domain}`
    : typeof window === "undefined"
      ? ""
      : window.location.origin;

  // One line, in a field that scrolls inside itself, for the reason a
  // connection string is: it is copied rather than read, and a command
  // wrapped over three lines is one somebody pastes two thirds of.
  const command = created
    ? `curl -sSL https://cubeship.dev/install.sh | sh -s -- --control-plane ${controlPlane} --token ${created.token}`
    : "";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      setCreated(await api.post<ClusterServerCreated>("/nodes", { name, description }));
      onAdded();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {created ? (
          <>
            <DialogHeader>
              <DialogTitle>Run this on {created.name}</DialogTitle>
              <DialogDescription>
                One command on the new machine. It installs Docker if it is missing, starts the
                daemon in worker mode, and the server appears here as soon as it calls in.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4 py-4">
              <CopyField
                label="On the new server, as root"
                value={command}
                hint="The credential is in this command and in no other. It is stored here only as a hash, so nothing can show it again."
              />
              <Notice>
                The machine needs to reach <code>{controlPlane}</code>. Nothing needs to reach it: a
                worker publishes no port.
              </Notice>
            </div>

            <DialogFooter>
              <Button type="button" onClick={() => onOpenChange(false)}>
                Done
              </Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={submit}>
            <DialogHeader>
              <DialogTitle>Add a server</DialogTitle>
              <DialogDescription>
                A second machine this instance manages. It holds no database and serves nothing of
                its own — it dials this control plane and does what it is told.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4 py-5">
              <ErrorAlert error={error} />
              <SlugField autoFocus value={name} onChange={setName} placeholder="eu-1" />
              <TextField
                label="Description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Frankfurt, 8 cores"
              />
            </div>

            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <ActionButton type="submit" busy={busy} disabled={!name}>
                Add
              </ActionButton>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
