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
          <SectionHeader
            title="Waiting"
            sub="Names this instance routes with no certificate behind them."
          />
          <Card className="mb-8 py-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Serves</TableHead>
                  <TableHead>Why</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.missing.map((m) => (
                  <TableRow key={m.host}>
                    <TableCell className="font-mono text-xs">
                      <div className="max-w-[20rem] whitespace-normal break-all">{m.host}</div>
                    </TableCell>
                    <TableCell className="text-xs">
                      <div className="max-w-[14rem] whitespace-normal break-all">
                        <Serves app={m.app} instance={m.instance} />
                      </div>
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

      {data?.traefik_says && data.traefik_says.length > 0 && (
        <>
          <SectionHeader
            title="What Traefik says"
            sub="The last distinct things it logged while trying to get certificates. This is the only place an ACME refusal is written down."
          />
          <Card className="mb-8">
            <div className="space-y-2 p-4">
              {data.traefik_says.map((line) => (
                <p
                  key={line}
                  className="overflow-x-auto whitespace-nowrap font-mono text-[11px] text-warning"
                >
                  {line}
                </p>
              ))}
            </div>
          </Card>
        </>
      )}

      <SectionHeader
        title="Issued"
        sub={
          data?.acme_email
            ? `Let's Encrypt has ${data.acme_email} as the contact for this instance.`
            : "Renewal is automatic, thirty days before expiry."
        }
      />
      <Card className="py-0">
        {report.isLoading ? (
          <LoadingRows />
        ) : data && data.certificates.length > 0 ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Serves</TableHead>
                <TableHead>Issuer</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead>State</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.certificates.map((c) => (
                <TableRow key={c.host}>
                  <TableCell className="font-mono text-xs">
                    <div className="max-w-[22rem] whitespace-normal break-all">
                      {c.host}
                      {c.sans && c.sans.length > 0 && (
                        <span className="ml-2 text-muted-foreground">+ {c.sans.join(", ")}</span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-xs">
                    <div className="max-w-[16rem] whitespace-normal break-all">
                      <Serves app={c.app} instance={c.instance} orphan={c.orphan} />
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
            Nothing issued yet. A certificate is asked for the first time somebody reaches an app at
            a name this instance serves.
          </p>
        )}
      </Card>

      <p className="mt-4 text-xs leading-relaxed text-muted-foreground">
        Traefik issues and renews these on its own, thirty days before expiry, and keeps them in its
        own store — there is nothing to press here. A name stuck waiting is usually one that does
        not resolve to this host, or an app that has not been deployed since the name was added.
      </p>
    </>
  );
}

// Why each missing name is missing, in the words the reason is worth
// reading in: what to do about it.
const WHY: Record<MissingReason, string> = {
  tls_not_configured: "This instance has no domain, so no certificate is asked for at all.",
  not_deployed:
    "Nothing is running with this name in its labels. A container keeps the routing it was created with, so Traefik has never been told about it — redeploy the app and it will be.",
  pending:
    "Traefik knows the name and has not got a certificate for it. Normal for a minute after a deploy; after that, check the name resolves to this host.",
};

// The registry is not an app, so "redeploy it" is not the answer: its
// container is the instance's own, and the daemon replaces it when the
// domain changes.
const WHY_REGISTRY_NOT_DEPLOYED =
  "The registry's container is not running with this name in its labels — it was made before the instance had a domain, or it is not running at all. Setting the domain again under Instance rebuilds it.";

// Who a name belongs to. An app is a link, because the next thing
// somebody does about it is open the app.
function Serves({ app, instance, orphan }: { app?: string; instance?: boolean; orphan?: boolean }) {
  if (orphan) {
    return <span className="text-muted-foreground">nothing — unused</span>;
  }
  if (instance) {
    return <span className="text-muted-foreground">this instance</span>;
  }
  if (!app) return <span className="text-muted-foreground">—</span>;
  return (
    <Link href={`/projects/${app}`} className="font-mono underline underline-offset-4">
      {app}
    </Link>
  );
}

// Traefik renews thirty days out, so a certificate still inside two
// weeks is one whose renewal is not working.
function state(c: Certificate): string {
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
