"use client";

import { useQuery } from "@tanstack/react-query";
import { InfoIcon } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { RowAction, RowActions } from "@/components/row-actions";
import { StatusBadge } from "@/components/status-badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { api, type CertificateReport, type MissingReason } from "@/lib/api";
import { message } from "@/lib/errors";

// Every name this instance routes, and where its certificate got to.
//
// **One table.** It was two — "Issued" above "Waiting" — which split the
// list by an answer rather than by a question: what somebody comes here
// to find out is whether a name is served, and that is a column. Split,
// a name moved between tables when it got one, and a name in neither was
// one you had to notice was absent.
//
// Nothing here issues, renews or deletes. Traefik does all of it and
// keeps the only store; what this adds is which name is which, and why
// one is still waiting.
export default function Certificates() {
  const report = useQuery({
    queryKey: ["certificates"],
    queryFn: () => api.get<CertificateReport>("/certificates"),
  });
  const [why, setWhy] = useState<Row | null>(null);

  const data = report.data;
  const rows = data ? entries(data) : null;

  const columns: Column<Row>[] = [
    {
      id: "name",
      header: "Name",
      width: 44,
      wrap: true,
      sortBy: (r) => r.host,
      cell: (r) => <span className="font-mono text-xs">{r.host}</span>,
    },
    {
      id: "issuer",
      header: "Issuer",
      width: 14,
      sortBy: (r) => r.issuer ?? "",
      cell: (r) => <Muted>{r.issuer}</Muted>,
    },
    {
      id: "expires",
      header: "Expires",
      width: 22,
      sortBy: (r) => r.notAfter ?? "",
      cell: (r) =>
        r.notAfter ? (
          <span className="whitespace-nowrap text-xs text-muted-foreground">
            <span className="font-mono">{day(r.notAfter)}</span> {remaining(r.notAfter)}
          </span>
        ) : (
          <Muted />
        ),
    },
    {
      id: "state",
      header: "State",
      width: 14,
      sortBy: (r) => r.state,
      cell: (r) => <StatusBadge value={r.state} />,
    },
    {
      id: "actions",
      header: "",
      width: 6,
      align: "right",
      // Only where there is something to say. A row action on every row
      // is a column of buttons; on some rows it is a mark that this one
      // has an answer the others do not.
      cell: (r) =>
        r.why ? (
          <RowActions>
            <RowAction icon={InfoIcon} label="Why it is waiting" onClick={() => setWhy(r)} />
          </RowActions>
        ) : null,
    },
  ];

  return (
    <>
      <ErrorAlert error={report.error ? message(report.error) : null} />

      {data && !data.tls_enabled && (
        <Notice tone="warning">
          No domain on this instance, so nothing is asked for. Set one under{" "}
          <Link href="/settings" className="underline underline-offset-4">
            Instance
          </Link>
          .
        </Notice>
      )}

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(r) => r.host}
        empty="Nothing yet. The first is asked for when an app is first reached at a name this instance serves."
      />

      <Dialog open={why !== null} onOpenChange={(open) => !open && setWhy(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm break-all">{why?.host}</DialogTitle>
          </DialogHeader>
          <p className="text-sm leading-relaxed text-muted-foreground">{why?.why}</p>
          {/* Traefik's own words, when it said any. It is the only place
              an ACME refusal is written down, and it scrolls rather than
              wraps because a log line is one long line by nature. */}
          {why?.detail && (
            <p className="overflow-x-auto whitespace-nowrap font-mono text-[11px] text-warning">
              {why.detail}
            </p>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function Muted({ children }: { children?: string }) {
  return <span className="font-mono text-xs text-muted-foreground">{children || "—"}</span>;
}

// --- the rows ---

type Row = {
  host: string;
  state: string;
  issuer?: string;
  notAfter?: string;
  why?: string;
  detail?: string;
};

// Both halves of the report as one list, with what is not served yet at
// the top: a certificate that exists needs nothing from anybody.
function entries(report: CertificateReport): Row[] {
  const waiting: Row[] = report.missing.map((m) => ({
    host: m.host,
    state: m.reason === "another_server" ? "elsewhere" : "waiting",
    why: m.instance && m.reason === "not_deployed" ? WHY_REGISTRY : WHY[m.reason],
    detail: m.detail,
  }));

  const issued: Row[] = report.certificates.map((c) => ({
    host: c.host,
    state: c.orphan ? "unused" : expiry(c.not_after),
    issuer: c.issuer,
    notAfter: c.not_after,
  }));

  return [...waiting, ...issued];
}

// Why a name has none yet: what to do about it, and then nothing.
const WHY: Record<MissingReason, string> = {
  tls_not_configured: "This instance has no domain, so no certificate is asked for.",
  not_deployed:
    "The name was added after the app's last deploy, and a container keeps the routing it was created with. Redeploy it.",
  pending:
    "Asked for and not answered. Retried every half hour, so it clears itself once the name resolves to this host — if it stays, that is what to check.",
  another_server: "Served by another machine in the cluster, which holds its own.",
};

// The registry is not an app, so "redeploy it" is not the answer: its
// container is the instance's own.
const WHY_REGISTRY =
  "The registry was built before this instance had a domain. Save the domain again under Instance to rebuild it.";

// Traefik renews thirty days out, so a certificate still inside two
// weeks is one whose renewal is not working.
function expiry(notAfter: string): string {
  const days = daysLeft(notAfter);
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
