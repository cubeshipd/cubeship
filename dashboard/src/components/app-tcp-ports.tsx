"use client";

import { CheckIcon, CopyIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { copyText } from "@/components/copy-button";
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
import { type App, type AppTCPPort, api } from "@/lib/api";
import { message } from "@/lib/errors";

// An app's published TCP ports: where it is reached for what is not HTTP.
//
// On the Network tab under the names, because it answers the same question
// for the protocols Traefik cannot route by name — and in the same shape:
// a table, with adding behind a dialog.
export function AppTCPPorts({
  app,
  ports,
  onChanged,
}: {
  app: App;
  ports: AppTCPPort[] | null;
  onChanged: () => void;
}) {
  const [adding, setAdding] = useState(false);
  const [added, setAdded] = useState(false);
  const [removing, setRemoving] = useState<AppTCPPort | null>(null);
  const [copied, setCopied] = useState<number | null>(null);

  const base = `/apps/${app.reference}/tcp-ports`;
  // Every port is published on this instance's own address, whichever
  // name the app answers at over HTTP.
  const addressOf = (p: AppTCPPort) =>
    app.address ? `${app.address}:${p.host_port}` : String(p.host_port);
  const cannotPublish =
    (ports?.length ?? 0) === 0 && (app.nodes.length > 1 || app.scale > 1 || app.autoscale.max > 0)
      ? "It runs as more than one copy, on more than one server or with autoscaling on. A host port is bound by one container, so put the app back to one copy on the control plane first."
      : null;

  async function copyAddress(p: AppTCPPort) {
    if (await copyText(addressOf(p))) {
      setCopied(p.id);
      setTimeout(() => setCopied((id) => (id === p.id ? null : id)), 1500);
    }
  }

  const columns: Column<AppTCPPort>[] = [
    {
      id: "container",
      header: "Container port",
      width: 30,
      sortBy: (p) => p.container_port,
      cell: (p) => <span className="font-mono text-xs">{p.container_port}</span>,
    },
    {
      id: "address",
      header: "Connect to",
      width: 50,
      sortBy: (p) => p.host_port,
      cell: (p) => <span className="font-mono text-xs">{addressOf(p)}</span>,
    },
    {
      id: "actions",
      header: "",
      width: 20,
      align: "right",
      cell: (p) => (
        <RowActions>
          <RowAction
            icon={copied === p.id ? CheckIcon : CopyIcon}
            label={`Copy ${addressOf(p)}`}
            onClick={() => copyAddress(p)}
          />
          <RowAction
            icon={Trash2Icon}
            label={`Stop publishing port ${p.container_port}`}
            danger
            onClick={() => setRemoving(p)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <SectionHeader
        title="TCP ports"
        sub="Ports of the container published on this instance's own address, for what is not HTTP — SSH into a Git server, a game server, a broker. Nothing proxies them and there is no TLS: the app's own authentication is what protects them."
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={cannotPublish !== null}
            onClick={() => setAdding(true)}
          >
            <PlusIcon />
            Publish port
          </Button>
        }
      />

      {cannotPublish && <Notice>{cannotPublish}</Notice>}

      <DataTable
        columns={columns}
        rows={ports}
        rowKey={(p) => String(p.id)}
        empty="This app publishes no TCP port."
      />

      {added && (
        <p className="mt-3 inline-flex items-center gap-1.5 text-xs text-success">
          <CheckIcon className="size-3.5" />
          Published. Redeploy to open it.
        </p>
      )}
      {(ports?.length ?? 0) > 0 && (
        <Notice className="mt-3">
          With a TCP port this app runs as one copy on {app.nodes[0]}, and each deploy stops the old
          container before the new one starts — the new one could not bind the port while the old
          one holds it — so the app is unavailable for a few seconds per deploy.
        </Notice>
      )}

      <PublishDialog
        base={base}
        open={adding}
        onOpenChange={(v) => {
          setAdding(v);
          if (v) setAdded(false);
        }}
        onSaved={() => {
          onChanged();
          setAdded(true);
        }}
      />

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(v) => !v && setRemoving(null)}
        title={`Stop publishing port ${removing?.container_port}?`}
        description="The container running now keeps the port open until the app is deployed again."
        confirmLabel="Stop publishing"
        onConfirm={async () => {
          if (!removing) return;
          await api.del(`${base}/${removing.id}`);
          setRemoving(null);
          onChanged();
        }}
      />
    </>
  );
}

// Publishing one port: what the app listens on, and where to put it.
function PublishDialog({
  base,
  open,
  onOpenChange,
  onSaved,
}: {
  base: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onSaved: () => void;
}) {
  const [containerPort, setContainerPort] = useState("");
  const [hostPort, setHostPort] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post<AppTCPPort>(base, {
        container_port: Number(containerPort),
        host_port: Number(hostPort) || 0,
      });
      setContainerPort("");
      setHostPort("");
      onOpenChange(false);
      onSaved();
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
            <DialogTitle>Publish a TCP port</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <TextField
              label="Container port"
              autoFocus
              inputMode="numeric"
              spellCheck={false}
              placeholder="22"
              value={containerPort}
              onChange={(e) => setContainerPort(e.target.value.replace(/\D/g, ""))}
              hint="What the app listens on inside its container."
            />
            <TextField
              label="Host port"
              inputMode="numeric"
              spellCheck={false}
              placeholder="17000–17999"
              value={hostPort}
              onChange={(e) => setHostPort(e.target.value.replace(/\D/g, ""))}
              hint="Where this instance publishes it, from 1024 to 65535. Leave it empty to have one picked. It opens on the next deploy."
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={containerPort === ""}>
              Publish
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
