"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingRows } from "@/components/loading";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { Card } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api, type Certificate, type CertificateReport, type MissingReason } from "@/lib/api";
import { message } from "@/lib/errors";

// What this instance holds, and what it is missing.
//
// Nothing on this page issues, renews or deletes anything: Traefik does
// all of it, thirty days before expiry, and the only store is its own
// file. So the page answers the two questions that store cannot — which
// certificate belongs to which app, and why a name that should have one
// does not.
export default function Certificates() {
  const report = useQuery({
    queryKey: ["certificates"],
    queryFn: () => api.get<CertificateReport>("/certificates"),
  });

  const data = report.data;

  return (
    <>
      <PageHeader title="Certificates" />

      {report.error && <ErrorAlert error={message(report.error)} />}

      {data && !data.tls_enabled && (
        <Notice tone="warning">
          This instance has no domain, so Traefik runs with no certificate resolver and asks for
          nothing. Apps are served over plain HTTP until one is set under{" "}
          <Link href="/settings" className="underline underline-offset-4">
            Instance
          </Link>
          .
        </Notice>
      )}

      {data && data.missing.length > 0 && (
        <>
          <SectionHeader title="Waiting" sub="Names routed here with no certificate yet." />
          <Card className="mb-8 py-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Why</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.missing.map((m) => (
                  <TableRow key={m.host}>
                    <TableCell className="font-mono text-xs">
                      <div className="max-w-[20rem] whitespace-normal break-all">{m.host}</div>
                    </TableCell>
                    {/* Bounded so the sentence wraps rather than pushing the
                        table sideways, and the quotation scrolls inside
                        itself — a log line is one long line by nature. */}
                    <TableCell className="text-xs text-muted-foreground">
                      <div className="max-w-[26rem] whitespace-normal">
                        {m.instance && m.reason === "not_deployed"
                          ? WHY_REGISTRY_NOT_DEPLOYED
                          : WHY[m.reason]}
                        {m.detail && (
                          <p className="mt-1 overflow-x-auto whitespace-nowrap font-mono text-[11px] text-warning">
                            {m.detail}
                          </p>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
        </>
      )}

      <SectionHeader title="Issued" sub="Renewed automatically, thirty days before expiry." />
      <Card className="py-0">
        {report.isLoading || (data && data.certificates.length > 0) ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Issuer</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead>State</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {/* Inside the table, which is where a <tr> can legally
                  go: rendered bare in the Card it was a hydration
                  error, and the point of the placeholder is to keep the
                  header from shifting when the rows land. */}
              {report.isLoading && <LoadingRows columns={4} />}
              {data?.certificates.map((c) => (
                <TableRow key={c.host}>
                  <TableCell className="font-mono text-xs">
                    <div className="max-w-[22rem] whitespace-normal break-all">
                      {c.host}
                      {c.sans && c.sans.length > 0 && (
                        <span className="ml-2 text-muted-foreground">+ {c.sans.join(", ")}</span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {c.issuer || "—"}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                    <span className="font-mono">{day(c.not_after)}</span>{" "}
                    <span>{remaining(c.not_after)}</span>
                  </TableCell>
                  <TableCell>
                    <StatusBadge value={state(c)} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <p className="p-6 text-sm text-muted-foreground">
            Nothing yet. The first one is asked for when an app is first reached at a name this
            instance serves.
          </p>
        )}
      </Card>
    </>
  );
}

// Why each missing name is missing — what to do about it, and then
// nothing. The long version of each of these was a paragraph in a table
// cell, which is a paragraph nobody reads on a screen they opened to
// find out whether something is wrong.
const WHY: Record<MissingReason, string> = {
  tls_not_configured: "No domain on this instance, so nothing is asked for.",
  not_deployed: "Added since the app's last deploy. Redeploy it.",
  pending:
    "Asked for and not answered yet. Retried every half hour — if it stays here, check that the name resolves to this host.",
  // Not a problem, and the only entry here that is not.
  another_server: "Served by another machine, which holds its own.",
};

// The registry is not an app, so "redeploy it" is not the answer: its
// container is the instance's own, and the daemon replaces it when the
// domain changes.
const WHY_REGISTRY_NOT_DEPLOYED =
  "The registry was built before this instance had a domain. Save the domain again under Instance to rebuild it.";

// Traefik renews thirty days out, so a certificate still inside two
// weeks is one whose renewal is not working.
//
// **Unused comes first**, before any of that. A certificate nothing
// answers at is not expiring in any sense worth a warning — and it is
// the one thing the "Serves" column said that the rest of the row does
// not, which is why it moved here when that column went: a state
// belongs in the state, and it costs no column.
function state(c: Certificate): string {
  if (c.orphan) return "unused";
  const days = daysLeft(c.not_after);
  if (days < 0) return "expired";
  if (days < 14) return "expiring";
  return "valid";
}

function daysLeft(notAfter: string): number {
  return Math.floor((new Date(notAfter).getTime() - Date.now()) / 86_400_000);
}

function day(value: string): string {
  return new Date(value).toISOString().slice(0, 10);
}

function remaining(notAfter: string): string {
  const days = daysLeft(notAfter);
  if (days < 0) return `${-days}d ago`;
  if (days === 0) return "today";
  return `in ${days}d`;
}
