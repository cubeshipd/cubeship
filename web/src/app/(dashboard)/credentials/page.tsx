"use client";

import { PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CredentialDialog } from "@/components/credential-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { api, type Credential, type CredentialProvider } from "@/lib/api";
import { CAPABILITY_LABELS, providerIcon } from "@/lib/credentials";
import { message } from "@/lib/errors";

// The accounts this instance is wired to.
//
// One secret, stored once. An AWS access key is the same key whether
// Route 53 writes a record with it or ECR is pulled from with it, and
// it used to be entered twice — once under DNS Providers and once under
// Registries — which is two places to rotate it and one to forget.
//
// What each can be used for is shown rather than chosen: it follows
// from the provider, and the pages that need one ask for the ones that
// can do their job.
export default function Credentials() {
  const [creds, setCreds] = useState<Credential[] | null>(null);
  const [providers, setProviders] = useState<CredentialProvider[]>([]);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Credential | null>(null);
  const [deleting, setDeleting] = useState<Credential | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<Credential[]>("/credentials")
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

  const columns: Column<Credential>[] = [
    {
      id: "label",
      header: "Label",
      width: 28,
      sortBy: (c) => c.label,
      cell: (c) => <span className="text-sm">{c.label}</span>,
    },
    {
      id: "provider",
      header: "Provider",
      width: 22,
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
      id: "capabilities",
      header: "Used for",
      width: 22,
      cell: (c) => (
        <span className="flex flex-wrap gap-1">
          {c.capabilities.map((capability) => (
            <code
              key={capability}
              className="border border-border bg-secondary/50 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground"
            >
              {CAPABILITY_LABELS[capability] ?? capability}
            </code>
          ))}
        </span>
      ),
    },
    {
      id: "in_use",
      header: "In use by",
      width: 20,
      cell: (c) =>
        c.in_use_by?.length ? (
          <span className="truncate text-xs text-muted-foreground">{c.in_use_by.join(", ")}</span>
        ) : (
          // Worth reading: a credential nothing uses is one you can
          // delete, and the only place that is knowable is here.
          <span className="text-xs text-subtle-foreground">nothing</span>
        ),
    },
    {
      id: "actions",
      header: "",
      width: 8,
      align: "right",
      cell: (c) => (
        <span className="flex justify-end">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Edit ${c.label}`}
            onClick={() => setEditing(c)}
          >
            <PencilIcon className="size-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Delete ${c.label}`}
            onClick={() => setDeleting(c)}
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
        title="Credentials"
        sub="The accounts this instance is wired to. One secret is stored once and used wherever it can be — an AWS access key reaches Route 53 and ECR both, so rotating it is one edit rather than one per thing that uses it."
        actions={
          <Button onClick={() => setAdding(true)}>
            <PlusIcon />
            New credential
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={creds}
        rowKey={(c) => String(c.id)}
        empty={
          <span className="flex items-center justify-between gap-4">
            No credentials yet. Add one and it becomes available to everything it can be used for.
            <Button variant="outline" onClick={() => setAdding(true)}>
              Add one
            </Button>
          </span>
        }
      />

      <CredentialDialog
        providers={providers}
        open={adding}
        onOpenChange={setAdding}
        onSaved={reload}
      />

      <CredentialDialog
        providers={providers}
        credential={editing ?? undefined}
        open={editing !== null}
        onOpenChange={(open) => !open && setEditing(null)}
        onSaved={reload}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`Delete ${deleting?.label}?`}
        description={
          deleting?.in_use_by?.length ? (
            <>
              <strong>{deleting.in_use_by.join(", ")}</strong> still authenticates with it, so the
              daemon will refuse. Point those elsewhere first.
            </>
          ) : (
            "The secret is removed from this instance. Nothing is using it, so nothing breaks."
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
