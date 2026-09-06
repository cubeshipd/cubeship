"use client";

import { PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CredentialDialog } from "@/components/credential-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
import { Button } from "@/components/ui/button";
import { api, type Credential } from "@/lib/api";
import { message } from "@/lib/errors";

// The secrets this instance holds.
//
// One secret, stored once. An AWS access key is the same key whether
// Route 53 writes a record with it or ECR is pulled from with it, and
// it used to be entered twice — once under DNS Providers and once under
// Registries — which is two places to rotate it and one to forget.
//
// A credential carries no provider: what it is used for is decided
// where it is wired up, and "In use by" is what reports the answer.
export default function Credentials() {
  const [creds, setCreds] = useState<Credential[] | null>(null);
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

  const columns: Column<Credential>[] = [
    {
      id: "label",
      header: "Label",
      width: 34,
      sortBy: (c) => c.label,
      cell: (c) => <span className="text-sm">{c.label}</span>,
    },
    {
      id: "username",
      header: "Username",
      width: 26,
      sortBy: (c) => c.username ?? "",
      cell: (c) =>
        c.username ? (
          <span className="font-mono text-xs">{c.username}</span>
        ) : (
          // A bare token is a normal credential, not a half-filled one.
          <span className="text-xs text-subtle-foreground">a bare token</span>
        ),
    },
    {
      id: "in_use",
      header: "In use by",
      width: 28,
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
      width: 12,
      align: "right",
      cell: (c) => (
        <RowActions>
          <RowAction icon={PencilIcon} label={`Edit ${c.label}`} onClick={() => setEditing(c)} />
          <RowAction
            icon={Trash2Icon}
            label={`Delete ${c.label}`}
            danger
            onClick={() => setDeleting(c)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="Credentials"
        actions={
          <Button onClick={() => setAdding(true)}>
            <PlusIcon />
            New credential
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable columns={columns} rows={creds} rowKey={(c) => String(c.id)} />

      <CredentialDialog open={adding} onOpenChange={setAdding} onSaved={reload} />

      <CredentialDialog
        credential={editing ?? undefined}
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
