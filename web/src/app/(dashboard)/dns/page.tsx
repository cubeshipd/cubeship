"use client";

import { KeyRoundIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { api, type DNSCredential, type DNSStatus } from "@/lib/api";
import { DNS_CREDENTIALS, providerIcon } from "@/lib/credentials";
import { message } from "@/lib/errors";

// The accounts this instance manages records through.
//
// There is nothing to create here. DNS has no configuration of its own
// — an account that can write records is a *credential* whose provider
// knows how to, so this page lists the credentials that can and sends
// anything about the account itself to Credentials, where it is stored
// once and reached by everything else that can use it.
export default function DNSProviders() {
  const [creds, setCreds] = useState<DNSCredential[] | null>(null);
  const [statuses, setStatuses] = useState<Record<number, DNSStatus>>({});
  const [error, setError] = useState<string | null>(null);
  const router = useRouter();

  const reload = useCallback(() => {
    api
      .get<DNSCredential[]>(DNS_CREDENTIALS)
      .then(setCreds)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

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
      width: 40,
      sortBy: (c) => c.label,
      cell: (c) => <span className="text-sm">{c.label}</span>,
    },
    {
      id: "provider",
      header: "Provider",
      width: 32,
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
      width: 28,
      // The reason is on the badge itself: a row that says unauthorized
      // and nothing else leaves you opening a screen to find out what
      // the provider actually said.
      cell: (c) => (
        <span title={statuses[c.id]?.detail}>
          <StatusBadge value={statuses[c.id]?.state ?? "checking"} />
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
          <Button
            variant="outline"
            nativeButton={false}
            render={
              <Link href="/credentials">
                <KeyRoundIcon />
                Credentials
              </Link>
            }
          />
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
            No credential here can write DNS records yet. Add one under Credentials and it appears
            here.
            <Button
              variant="outline"
              nativeButton={false}
              render={<Link href="/credentials">Add one</Link>}
            />
          </span>
        }
      />
    </>
  );
}
