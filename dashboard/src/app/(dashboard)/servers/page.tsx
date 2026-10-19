"use client";

import { HardDriveIcon, PlusIcon, TerminalIcon, Trash2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CopyField } from "@/components/copy-field";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { RowAction, RowActions } from "@/components/row-actions";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
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
  canRootShell,
  formatBytes,
  formatCPU,
  type Settings,
} from "@/lib/api";
import { message } from "@/lib/errors";
import { useOpenOnArrival } from "@/lib/open-on-arrival";

// How often the cluster is re-read. Matched to the agents' own cadence:
// asking faster than the machines call in is two requests for one
// answer.
const REFRESH_MS = 10_000;

// The nodes this instance is made of.
//
// The control plane is in the table rather than beside it. It is a
// node like the others — the same daemon, the same numbers — and it
// differs in one thing that the list already says: it is the one that
// decides. A screen listing only "the other nodes" would be a screen
// that cannot answer where something runs.
export default function Nodes() {
  const me = useSession();
  const router = useRouter();
  const [servers, setServers] = useState<ClusterServer[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  useOpenOnArrival("new", setAdding);
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
      header: "Node",
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
      id: "actions",
      header: "",
      width: 6,
      align: "right",
      // The control plane is not a machine this instance joined, so
      // there is nothing to remove it from. A missing button explains
      // nothing, but neither does a disabled one on a row whose whole
      // label already says why.
      //
      // A root shell is an admin's alone, so nobody else is shown the
      // button; a machine that is not calling in cannot be reached, so
      // its button says why rather than opening a terminal that fails.
      cell: (s) => (
        <RowActions>
          {canRootShell(me) && (
            <RowAction
              icon={TerminalIcon}
              label={`Open a root shell on node ${s.name}`}
              disabled={s.status !== "ready"}
              title={
                s.status === "ready"
                  ? undefined
                  : "This node is not calling in, so nothing can open a shell on it."
              }
              onClick={() => router.push(`/servers/${encodeURIComponent(s.name)}/shell`)}
            />
          )}
          {canRootShell(me) && s.control_plane && s.status === "ready" && (
            <RowAction
              icon={HardDriveIcon}
              label={`Manage Docker storage on node ${s.name}`}
              onClick={() => router.push(`/servers/${encodeURIComponent(s.name)}/storage`)}
            />
          )}
          {!s.control_plane && (
            <RowAction
              icon={Trash2Icon}
              label={`Remove node ${s.name}`}
              danger
              onClick={() => setRemoving(s)}
            />
          )}
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <SectionHeader
        page
        title="Nodes"
        sub="Connect and manage the machines in this instance. The control plane appears alongside worker nodes."
        actions={
          <Button onClick={() => setAdding(true)}>
            <PlusIcon />
            Connect Node
          </Button>
        }
      />
      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={servers}
        search={{
          placeholder: "Search nodes",
          countLabel: { singular: "node", plural: "nodes" },
          by: (s) => [s.name, s.address, s.version],
        }}
        rowKey={(s) => s.name}
        loadingRows={2}
        empty="No nodes yet."
        className="mb-4"
      />

      <AddDialog open={adding} onOpenChange={setAdding} onAdded={reload} />

      {/* No word to type. Removing a node is a local act — the row
          goes and its credential with it, and nothing on that machine
          is touched — so the guard only has to stop the misclick. */}
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title="Remove this node?"
        confirmLabel="Remove"
        holdToConfirm
        description={
          <>
            Remove <code className="text-foreground">{removing?.name}</code> from the cluster. Its
            agent can no longer connect, but anything already running on the machine keeps running.
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
// have copied it would mean removing the node and adding it back.
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

  // Where the new node will dial. The instance's domain when it has
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
            <DialogHeader className="mb-0 gap-1">
              <DialogTitle>Run this on {created.name}</DialogTitle>
              <DialogDescription>
                Run this command on the new machine to connect it to the control plane.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-2 py-1">
              <CopyField
                label="On the new node, as root"
                value={command}
                hint="The credential is in this command and in no other. It is stored here only as a hash, so nothing can show it again."
              />
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
              <DialogTitle>Connect a node</DialogTitle>
              <DialogDescription>
                Connect a worker machine to this control plane. It dials this instance and runs
                workloads assigned to it.
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
                Connect Node
              </ActionButton>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
