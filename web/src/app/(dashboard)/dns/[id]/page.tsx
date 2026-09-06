"use client";

import { ChevronLeftIcon, ChevronRightIcon, KeyRoundIcon } from "lucide-react";
import Link from "next/link";
import { use, useCallback, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList, LoadingNote } from "@/components/loading";
import { PageHeader } from "@/components/page-header";
import { SearchBar } from "@/components/search-bar";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api, type DNSProvider, type DNSZone } from "@/lib/api";
import { providerIcon } from "@/lib/credentials";
import { message } from "@/lib/errors";

// One provider's zones. Clicking one opens it.
//
// The zones used to expand in place, which was what a static export
// could do: there was no route to send you to. A zone holds tens or
// hundreds of records and is the thing you actually work in, so it is a
// page of its own — reachable by a link you can send someone.
export default function DNSZones({ params }: PageProps<"/dns/[id]">) {
  const { id } = use(params);

  const [provider, setProvider] = useState<DNSProvider | null>(null);
  const [zones, setZones] = useState<DNSZone[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  const base = `/dns/${id}`;

  useEffect(() => {
    api
      .get<DNSProvider[]>("/dns")
      .then((list) => setProvider(list.find((p) => String(p.id) === id) ?? null))
      .catch(() => setProvider(null));
  }, [id]);

  const load = useCallback(() => {
    if (!base) return;
    setError(null);
    api
      .get<DNSZone[]>(`${base}/zones`)
      .then(setZones)
      .catch((e) => setError(message(e)));
  }, [base]);
  useEffect(load, [load]);

  const Icon = providerIcon(provider?.provider ?? "");

  const filtered = zones
    ? zones.filter((z) => z.name.toLowerCase().includes(query.trim().toLowerCase()))
    : [];

  return (
    <>
      <Link
        href="/dns"
        className="mb-4 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      >
        <ChevronLeftIcon className="size-3.5" />
        DNS Providers
      </Link>

      <PageHeader
        title={provider?.provider_name || "DNS provider"}
        icon={<Icon className="size-5 shrink-0 text-muted-foreground" />}
        actions={
          <Button
            variant="outline"
            nativeButton={false}
            render={
              // The secret is edited where it lives, not here: this
              // credential may be pulling images somewhere else at the
              // same time.
              <Link href="/credentials">
                <KeyRoundIcon />
                Credential
              </Link>
            }
          />
        }
        below={
          zones &&
          zones.length > 0 && (
            <SearchBar
              value={query}
              onChange={setQuery}
              placeholder="Filter zones"
              trailing={
                <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
                  {filtered.length}/{zones.length}
                </span>
              }
            />
          )
        }
      />

      <ErrorAlert error={error} />

      {zones === null && !error && (
        <div>
          <LoadingList rows={4} />
          <LoadingNote>Asking the provider which zones it holds</LoadingNote>
        </div>
      )}

      {zones?.length === 0 && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            This credential reaches no zones. A narrowly scoped token sees only what it was scoped
            to — which may be nothing, if the zone it was made for has since gone.
          </CardContent>
        </Card>
      )}

      {zones && zones.length > 0 && filtered.length === 0 && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            Nothing matches “{query}”.
          </CardContent>
        </Card>
      )}

      {filtered.length > 0 && (
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="px-4">Zone</TableHead>
                <TableHead className="px-4" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((zone) => (
                <TableRow key={zone.id} className="select-none">
                  {/* The link fills the cell rather than wrapping the
                      row: a row is not a valid parent for an anchor, and
                      an onClick handler on one is a link you cannot
                      middle-click, copy, or open in a new tab. */}
                  <TableCell className="p-0">
                    <Link
                      href={`/dns/${id}/zones/${encodeURIComponent(zone.name)}`}
                      className="block px-4 py-2.5 font-mono text-xs"
                    >
                      {zone.name}
                    </Link>
                  </TableCell>
                  <TableCell className="p-0 text-right">
                    <Link
                      href={`/dns/${id}/zones/${encodeURIComponent(zone.name)}`}
                      className="block px-4 py-2.5"
                      aria-hidden="true"
                      tabIndex={-1}
                    >
                      <ChevronRightIcon className="ml-auto size-3.5 text-muted-foreground" />
                    </Link>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </>
  );
}
