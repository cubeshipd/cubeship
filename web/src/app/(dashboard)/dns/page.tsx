"use client";

import { PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CredentialDialog } from "@/components/credential-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { api, type CredentialProvider, type DNSCredential, type DNSStatus } from "@/lib/api";
import { DNS_CREDENTIALS, providerIcon } from "@/lib/credentials";
import { message } from "@/lib/errors";

// The accounts this instance manages records through.
//
// DNS has no configuration of its own: what there is to add, rename or
// delete is the *account*, and an account is a credential. So this page
// lists the credentials that can write records and edits them in place
// — the same rows the Credentials screen holds, reached from where you
// were already standing.
//
// It is deliberately not a link to that screen. A credential is a
// convenience, not a prerequisite: being sent somewhere else to make an
// account before you can add a DNS provider is the tail wagging the
// dog, and it is exactly what this page did wrong for one release.
export default function DNSProviders() {
  const [creds, setCreds] = useState<DNSCredential[] | null>(null);
  const [statuses, setStatuses] = useState<Record<number, DNSStatus>>({});
  const [providers, setProviders] = useState<CredentialProvider[]>([]);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<DNSCredential | null>(null);
  const [deleting, setDeleting] = useState<DNSCredential | null>(null);
  const [error, setError] = useState<string | null>(null);
  const router = useRouter();

  const reload = useCallback(() => {
    api
      .get<DNSCredential[]>(DNS_CREDENTIALS)
      .then(setCreds)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  // Loaded with the page rather than when the dialog opens: the form's
  // first field is built from these, and a select with nothing in it
  // yet is a disabled select.
  useEffect(() => {
    api
      .get<CredentialProvider[]>("/credentials/providers")
      .then(setProviders)
      .catch((e) => setError(message(e)));
  }, []);

  // One probe per row, in parallel, after the list is on screen. Each is
  // a live round trip to someone else's API, so waiting for all of them
  // before drawing anything would make the page as slow as the slowest
  // provider in it.
  useEffect(() => {
    if (!creds) return;
    for (const c of creds) {
      api
        .get<DNSStatus>(`/dns/${c.id}/status`)
        .then((s) => setStatuses((prev) => ({ ...prev, [c.id]: s })))
        .catch((e) =>
          setStatuses((prev) => ({
            ...prev,
            [c.id]: { state: "unreachable", detail: message(e) },
          })),
        );
    }
  }, [creds]);

  const columns: Column<DNSCredential>[] = [
    {
      id: "label",
      header: "Label",
      width: 34,
      sortBy: (c) => c.label,
      cell: (c) => <span className="text-sm">{c.label}</span>,
    },
    {
      id: "provider",
      header: "Provider",
      width: 28,
      sortBy: (c) => c.provider_name,
      cell: (c) => {
        const Icon = providerIcon(c.provider);
        return (
          <span className="flex items-center gap-2 text-sm">
            <Icon className="size-4 shrink-0" />
            {c.provider_name}
          </span>
        );
      },
    },
    {
      id: "status",
      header: "Status",
      width: 20,
      // The reason is on the badge itself: a row that says unauthorized
      // and nothing else leaves you opening a screen to find out what
      // the provider actually said.
      cell: (c) => (
        <span title={statuses[c.id]?.detail}>
          <StatusBadge value={statuses[c.id]?.state ?? "checking"} />
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 18,
      align: "right",
      // The row opens the zones, so each button stops the click going
      // any further: pressing Edit is not also asking for the zones.
      // Stopped on the buttons rather than on a wrapper, which would
      // be a click handler on something nothing can focus.
      cell: (c) => (
        <span className="flex justify-end">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Edit ${c.label}`}
            onClick={(e) => {
              e.stopPropagation();
              setEditing(c);
            }}
          >
            <PencilIcon className="size-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Delete ${c.label}`}
            onClick={(e) => {
              e.stopPropagation();
              setDeleting(c);
            }}
          >
            <Trash2Icon className="size-3.5" />
          </Button>
        </span>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="DNS Providers"
        sub={
          <>
            The DNS accounts this instance manages records through. Cubeship already asks you to
            point a name at this host — this is where that happens, rather than in somebody
            else&apos;s control panel.
          </>
        }
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
        rows={creds}
        rowKey={(c) => String(c.id)}
        onRowClick={(c) => router.push(`/dns/${c.id}`)}
        empty={
          <span className="flex items-center justify-between gap-4">
            No DNS provider yet. Add the account Cubeship should read and write records through.
            <Button variant="outline" onClick={() => setAdding(true)}>
              Add one
            </Button>
          </span>
        }
      />

      {/* Both dialogs write to /credentials, because that is where the
          account lives. Adding one here puts it on the Credentials
          screen too, where an AWS key added for Route 53 is the same
          key ECR is pulled with. */}
      <CredentialDialog
        providers={providers}
        capability="dns"
        title="New DNS provider"
        description="The account Cubeship reads and writes records through. It is stored as a credential, so the same account is there for anything else it can reach — an AWS key writes Route 53 records and pulls from ECR."
        open={adding}
        onOpenChange={setAdding}
        onSaved={reload}
      />

      <CredentialDialog
        providers={providers}
        credential={editing ?? undefined}
        capability="dns"
        open={editing !== null}
        onOpenChange={(open) => !open && setEditing(null)}
        onSaved={reload}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`Delete ${deleting?.label}?`}
        confirmWord={deleting?.label}
        description={
          deleting?.in_use_by?.length ? (
            <>
              <strong>{deleting.in_use_by.join(", ")}</strong> still authenticates with this
              account, so the daemon will refuse. Point those elsewhere first.
            </>
          ) : (
            "Your records stay exactly as they are. What goes is this instance's ability to read or write them."
          )
        }
        onConfirm={async () => {
          if (deleting) await api.del(`/credentials/${deleting.id}`);
          setDeleting(null);
          reload();
        }}
      />
    </>
  );
}
