"use client";

import { PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { DNSProviderDialog } from "@/components/dns-provider-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import {
  api,
  type Credential,
  type DNSProvider,
  type DNSProviderKind,
  type DNSStatus,
} from "@/lib/api";
import { providerIcon } from "@/lib/credentials";
import { message } from "@/lib/errors";

// The providers this instance manages records through.
//
// A row is which API to speak and which stored credential to speak it
// with — the secret itself is a credential, so the same AWS key can be
// writing records here and pulling images under Registries.
//
// Adding one asks both questions in place, and the credential field
// offers "type a new one". It is deliberately not a link to the
// Credentials screen: a credential is a convenience, not a
// prerequisite, and being sent somewhere else to make one first is the
// tail wagging the dog — which is exactly what this page did wrong for
// one release.
export default function DNSProviders() {
  const [providers, setProviders] = useState<DNSProvider[] | null>(null);
  const [statuses, setStatuses] = useState<Record<number, DNSStatus>>({});
  const [kinds, setKinds] = useState<DNSProviderKind[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<DNSProvider | null>(null);
  const [deleting, setDeleting] = useState<DNSProvider | null>(null);
  const [error, setError] = useState<string | null>(null);
  const router = useRouter();

  const reload = useCallback(() => {
    api
      .get<DNSProvider[]>("/dns")
      .then(setProviders)
      .catch((e) => setError(message(e)));
    api
      .get<Credential[]>("/credentials")
      .then(setCredentials)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  // Loaded with the page rather than when the dialog opens: the form's
  // first field is built from these, and a select with nothing in it
  // yet is a disabled select.
  useEffect(() => {
    api
      .get<DNSProviderKind[]>("/dns/providers")
      .then(setKinds)
      .catch((e) => setError(message(e)));
  }, []);

  // One probe per row, in parallel, after the list is on screen. Each is
  // a live round trip to someone else's API, so waiting for all of them
  // before drawing anything would make the page as slow as the slowest
  // provider in it.
  useEffect(() => {
    if (!providers) return;
    for (const p of providers) {
      api
        .get<DNSStatus>(`/dns/${p.id}/status`)
        .then((s) => setStatuses((prev) => ({ ...prev, [p.id]: s })))
        .catch((e) =>
          setStatuses((prev) => ({
            ...prev,
            [p.id]: { state: "unreachable", detail: message(e) },
          })),
        );
    }
  }, [providers]);

  const columns: Column<DNSProvider>[] = [
    {
      id: "provider",
      header: "Provider",
      width: 32,
      sortBy: (p) => p.provider_name,
      cell: (p) => {
        const Icon = providerIcon(p.provider);
        return (
          <span className="flex items-center gap-2 text-sm">
            <Icon className="size-4 shrink-0" />
            {p.provider_name}
          </span>
        );
      },
    },
    {
      id: "credential",
      header: "Credential",
      width: 30,
      sortBy: (p) => p.label,
      cell: (p) => <span className="text-sm">{p.label}</span>,
    },
    {
      id: "status",
      header: "Status",
      width: 20,
      // The reason is on the badge itself: a row that says unauthorized
      // and nothing else leaves you opening a screen to find out what
      // the provider actually said.
      cell: (p) => (
        <span title={statuses[p.id]?.detail}>
          <StatusBadge value={statuses[p.id]?.state ?? "checking"} />
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 18,
      align: "right",
      cell: (p) => (
        <RowActions>
          <RowAction
            icon={PencilIcon}
            label={`Edit ${p.provider_name}`}
            onClick={() => setEditing(p)}
          />
          <RowAction
            icon={Trash2Icon}
            label={`Disconnect ${p.provider_name}`}
            danger
            onClick={() => setDeleting(p)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="DNS Providers"
        actions={
          <Button onClick={() => setAdding(true)}>
            <PlusIcon />
            New DNS provider
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={providers}
        rowKey={(p) => String(p.id)}
        onRowClick={(p) => router.push(`/dns/${p.id}`)}
      />

      <DNSProviderDialog
        kinds={kinds}
        credentials={credentials}
        open={adding}
        onOpenChange={setAdding}
        onSaved={reload}
      />

      <DNSProviderDialog
        kinds={kinds}
        credentials={credentials}
        provider={editing ?? undefined}
        open={editing !== null}
        onOpenChange={(open) => !open && setEditing(null)}
        onSaved={reload}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`Disconnect ${deleting?.provider_name}?`}
        confirmWord={deleting?.provider_name}
        confirmLabel="Disconnect"
        description="Your records stay exactly as they are. What goes is this instance's ability to read or write them — the credential itself is left alone."
        onConfirm={async () => {
          if (deleting) await api.del(`/dns/${deleting.id}`);
          setDeleting(null);
          reload();
        }}
      />
    </>
  );
}
