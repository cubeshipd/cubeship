"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { Notice } from "@/components/notice";
import { SearchableSelect } from "@/components/searchable-select";
import { TextField } from "@/components/text-field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, type DNSProvider, type DNSRecord, type DNSZone, type Settings } from "@/lib/api";
import { providerIcon } from "@/lib/credentials";

// The instance's own domain, beside a DNS provider and a name somewhere
// else: every name under it already resolves here, so it writes no record.
const INSTANCE_DOMAIN = "instance";

// A record to write before the name is used: an A for host, in a zone a
// connected provider holds.
export type PendingRecord = { providerID: string; zoneID: string; host: string };

// writeRecord points a pending record's host at this instance.
export function writeRecord(r: PendingRecord, ip: string) {
  return api.put(`/dns/${r.providerID}/records?zone=${encodeURIComponent(r.zoneID)}`, {
    name: r.host,
    type: "A",
    values: [ip],
    ttl: 300,
  });
}

// A hostname, chosen the way an app's own domain is: through a connected
// DNS provider — a zone and a subdomain, whose record the caller writes —
// under the instance's own domain, or typed for DNS kept elsewhere.
export function DomainInput({
  label,
  hint,
  value,
  onChange,
  onRecord,
  suggested,
  settings,
  address,
}: {
  label: string;
  hint?: string;
  value: string;
  onChange: (host: string) => void;
  // The record this choice needs written, or null for none.
  onRecord: (record: PendingRecord | null) => void;
  // A name under the instance's own domain, empty when there is none.
  suggested?: string;
  settings?: Settings;
  // Where a record has to point: this instance's public address.
  address?: string;
}) {
  const [providerID, setProviderID] = useState(
    settings?.dns_provider_id || (suggested ? INSTANCE_DOMAIN : ""),
  );
  // Settings arrive after the first render, so the starting choice follows
  // them until somebody picks one.
  const [touched, setTouched] = useState(false);
  useEffect(() => {
    if (!touched) setProviderID(settings?.dns_provider_id || (suggested ? INSTANCE_DOMAIN : ""));
  }, [touched, settings?.dns_provider_id, suggested]);
  const [zoneID, setZoneID] = useState("");
  const [subdomain, setSubdomain] = useState("");
  const [manual, setManual] = useState(value);

  const onInstanceDomain = providerID === INSTANCE_DOMAIN;
  const automatic = providerID !== "" && !onInstanceDomain;

  const providers = useQuery({ queryKey: ["dns"], queryFn: () => api.get<DNSProvider[]>("/dns") });
  const zones = useQuery({
    queryKey: ["dns", providerID, "zones"],
    queryFn: () => api.get<DNSZone[]>(`/dns/${providerID}/zones`),
    enabled: automatic,
  });
  const zone = zones.data?.find((z) => z.id === zoneID) ?? null;
  const records = useQuery({
    queryKey: ["dns", providerID, "records", zoneID],
    queryFn: () =>
      api.get<DNSRecord[]>(`/dns/${providerID}/records?zone=${encodeURIComponent(zoneID)}`),
    enabled: automatic && zoneID !== "",
  });

  const host = onInstanceDomain
    ? (suggested ?? "")
    : automatic
      ? zone && subdomain.trim()
        ? `${subdomain.trim()}.${zone.name}`
        : ""
      : manual.trim();
  const occupied = useMemo(
    () => (records.data ?? []).filter((r) => r.name === host),
    [records.data, host],
  );

  // The callbacks are read through a ref: a caller passes new ones on every
  // render, and an effect keyed on them would report on every render too.
  const report = useRef({ onChange, onRecord });
  report.current = { onChange, onRecord };
  useEffect(() => {
    if (host !== value) report.current.onChange(host);
  }, [host, value]);
  const pending = automatic && zone && host ? `${providerID}\n${zoneID}\n${host}` : "";
  useEffect(() => {
    const [p, z, h] = pending.split("\n");
    report.current.onRecord(pending ? { providerID: p, zoneID: z, host: h } : null);
  }, [pending]);

  return (
    <div className="space-y-3 sm:col-span-2">
      <div className="grid gap-3 sm:grid-cols-2">
        <SearchableSelect
          label={label}
          hint={hint}
          placeholder="My DNS is elsewhere"
          empty="No DNS providers connected yet."
          value={providerID}
          onChange={(v) => {
            setTouched(true);
            setProviderID(v);
            setZoneID("");
          }}
          busy={providers.isLoading}
          choices={[
            { value: "", label: "My DNS is elsewhere", hint: "Type the name" },
            ...(suggested
              ? [
                  {
                    value: INSTANCE_DOMAIN,
                    label: "This instance's domain",
                    hint: settings?.wildcard_domain
                      ? "Resolves here already"
                      : "Needs a wildcard record",
                  },
                ]
              : []),
            ...(providers.data ?? []).map((p) => ({
              value: String(p.id),
              label: p.provider_name,
              hint: p.label,
              icon: providerIcon(p.provider),
            })),
          ]}
        />
        {onInstanceDomain ? (
          <div className="space-y-2">
            <Label className="text-xs text-muted-foreground">Name</Label>
            <div className="flex h-10 items-center overflow-x-auto border border-border bg-secondary/40 px-3 font-mono text-sm text-muted-foreground">
              {suggested}
            </div>
          </div>
        ) : automatic ? (
          <SearchableSelect
            label="Zone"
            placeholder="Pick a domain"
            empty="This credential reaches no zones."
            value={zoneID}
            onChange={setZoneID}
            busy={zones.isLoading}
            choices={(zones.data ?? []).map((z) => ({ value: z.id, label: z.name }))}
          />
        ) : (
          <TextField
            label="Name"
            spellCheck={false}
            placeholder="app.example.com"
            value={manual}
            onChange={(e) => setManual(e.target.value)}
          />
        )}
      </div>

      {automatic && (
        <div className="space-y-2">
          <Label className="text-xs text-muted-foreground">Subdomain</Label>
          <div className="flex items-center gap-2">
            <Input
              aria-label="Subdomain"
              className="h-10 flex-1 px-3 text-sm"
              spellCheck={false}
              placeholder="app"
              disabled={!zone}
              value={subdomain}
              onChange={(e) => setSubdomain(e.target.value)}
            />
            <span className="shrink-0 font-mono text-sm text-muted-foreground">
              .{zone?.name ?? "zone"}
            </span>
          </div>
        </div>
      )}

      {automatic && host && (
        <div className="border border-border px-3 py-2 font-mono text-xs">
          <span className="text-muted-foreground">A</span>
          <span className="ml-4">{host}</span>
          <span className="ml-4 text-muted-foreground">{address || "—"}</span>
          {occupied.length > 0 && (
            <span className="ml-4 text-warning">
              now {occupied[0].type} {occupied[0].values.join(", ")}, replaced on install
            </span>
          )}
        </div>
      )}
      {automatic && !address && (
        <Notice tone="warning" className="mb-0">
          This instance does not know its own public address, so there is nothing to point the
          record at. Set it under{" "}
          <Link href="/settings" className="underline underline-offset-4">
            Settings
          </Link>
          .
        </Notice>
      )}
      {!automatic && !onInstanceDomain && (
        <p className="text-xs text-subtle-foreground">
          {address
            ? `The name has to resolve to this instance, ${address}, for the app to answer at it.`
            : "The name has to resolve to this instance for the app to answer at it."}
        </p>
      )}
    </div>
  );
}
