"use client";

import { useQuery } from "@tanstack/react-query";
import { CheckIcon, GlobeIcon } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { useOrg } from "@/components/org-context";
import { SearchableSelect } from "@/components/searchable-select";
import { TextField } from "@/components/text-field";
import { Card, CardContent } from "@/components/ui/card";
import { api, type DNSCredential, type DNSRecord, type DNSZone, type Settings } from "@/lib/api";
import { DNS_PROVIDERS } from "@/lib/dns";
import { message } from "@/lib/errors";

// Pointing the instance's domain at this host, through a DNS provider
// the organization has already connected.
//
// Two records and no more, ever: an A on the name itself and an A on the
// wildcard under it. That is the whole reason the domain is a subdomain
// rather than an apex — the registry is `registry.<domain>` and anything
// Cubeship grows later is `<something>.<domain>`, so the wildcard covers
// what does not exist yet and the operator never comes back here.
//
// The zone is found by matching, not asked for: a domain belongs to
// exactly one of the zones a credential reaches, and asking someone to
// pick it is asking them to repeat what they already typed.
export function InstanceDNS({
  settings,
  onSaved,
}: {
  settings: Settings;
  onSaved: (s: Settings) => void;
}) {
  const { org } = useOrg();
  const domain = settings.domain;

  const [providerID, setProviderID] = useState(settings.dns_provider_id ?? "");
  const [ip, setIP] = useState(settings.public_ip ?? "");
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setProviderID(settings.dns_provider_id ?? "");
    setIP(settings.public_ip ?? "");
  }, [settings]);

  const providers = useQuery({
    queryKey: ["dns", org],
    queryFn: () => api.get<DNSCredential[]>(`/orgs/${org}/dns`),
    enabled: Boolean(org),
  });

  const zones = useQuery({
    queryKey: ["dns", org, providerID, "zones"],
    queryFn: () => api.get<DNSZone[]>(`/orgs/${org}/dns/${providerID}/zones`),
    enabled: Boolean(org && providerID),
  });

  // The longest zone that the domain sits inside. Longest, because an
  // account can hold both `example.com` and `eu.example.com`, and a
  // record written to the wider one would be shadowed by the narrower.
  const zone = useMemo(() => {
    if (!domain || !zones.data) return null;
    return (
      zones.data
        .filter((z) => domain === z.name || domain.endsWith(`.${z.name}`))
        .sort((a, b) => b.name.length - a.name.length)[0] ?? null
    );
  }, [domain, zones.data]);

  const records = useQuery({
    queryKey: ["dns", org, providerID, "records", zone?.id],
    queryFn: () => api.get<DNSRecord[]>(`/orgs/${org}/dns/${providerID}/records?zone=${zone?.id}`),
    enabled: Boolean(org && providerID && zone),
  });

  // What is already at the two names, whatever type it is: a CNAME where
  // an A is going is just as much in the way as another A, and quietly
  // adding a second answer beside it is how a name starts resolving to
  // two places.
  const wildcard = `*.${domain}`;
  const occupied = (records.data ?? []).filter((r) => r.name === domain || r.name === wildcard);
  const alreadyRight =
    occupied.length === 2 &&
    occupied.every((r) => r.type === "A" && r.values.length === 1 && r.values[0] === ip);

  async function write() {
    setBusy(true);
    setError(null);
    setDone(false);
    try {
      for (const name of [domain, wildcard]) {
        await api.put(`/orgs/${org}/dns/${providerID}/records?zone=${zone?.id}`, {
          name,
          type: "A",
          values: [ip],
          ttl: 300,
        });
      }
      // Remembered so the next screen that needs this instance's DNS
      // does not ask again.
      onSaved(await api.put<Settings>("/settings", { dns_provider_id: providerID, public_ip: ip }));
      await records.refetch();
      setDone(true);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
    setConfirming(false);
  }

  if (!domain) {
    return (
      <Card>
        <CardContent className="py-2 text-sm text-muted-foreground">
          Set the domain above first. There is nothing to point anywhere until there is a name.
        </CardContent>
      </Card>
    );
  }

  const shape = providers.data?.find((p) => String(p.id) === providerID)?.provider;

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
          <ErrorAlert error={error} className="mb-0" />

          <div className="grid grid-cols-12 items-start gap-4">
            <SearchableSelect
              label="DNS provider"
              fieldClassName="col-span-7"
              placeholder="My DNS is elsewhere"
              empty="No DNS providers connected yet."
              value={providerID}
              onChange={setProviderID}
              busy={providers.isLoading}
              choices={(providers.data ?? []).map((p) => ({
                value: String(p.id),
                label: p.label,
                hint: DNS_PROVIDERS[p.provider]?.label,
                icon: DNS_PROVIDERS[p.provider]?.icon,
              }))}
            />
            <TextField
              label="Points at"
              fieldClassName="col-span-5"
              spellCheck={false}
              value={ip}
              onChange={(e) => setIP(e.target.value)}
              hint={
                settings.public_ip_configured
                  ? "Set by you."
                  : "Read off this host's own interface. Correct it if this box is behind NAT."
              }
            />
          </div>

          {!providerID && (
            <p className="text-xs leading-relaxed text-muted-foreground">
              Create these two records yourself, wherever your DNS lives — or{" "}
              <Link href="/dns" className="text-foreground underline underline-offset-4">
                connect a provider
              </Link>{" "}
              and Cubeship will write them.
            </p>
          )}

          {/* The records, always — as the thing to be written, or as the
              thing to copy into someone else's control panel. */}
          <div className="border border-border">
            {[domain, wildcard].map((name) => {
              const existing = occupied.find((r) => r.name === name);
              return (
                <div
                  key={name}
                  className="flex items-center gap-4 border-b border-border px-3 py-2 font-mono text-xs last:border-0"
                >
                  <span className="w-8 shrink-0 text-muted-foreground">A</span>
                  <span className="min-w-0 flex-1 truncate">{name}</span>
                  <span className="shrink-0 text-muted-foreground">{ip || "—"}</span>
                  {existing && (
                    <span
                      className={`w-40 shrink-0 truncate text-right text-[11px] ${
                        existing.type === "A" && existing.values[0] === ip
                          ? "text-success"
                          : "text-warning"
                      }`}
                    >
                      now {existing.type} {existing.values.join(", ")}
                    </span>
                  )}
                </div>
              );
            })}
          </div>

          {providerID && !zone && zones.isSuccess && (
            <p className="text-xs leading-relaxed text-warning">
              This credential reaches no zone that <code>{domain}</code> sits inside. Either the
              zone is on another account, or the token was scoped to a different one.
            </p>
          )}

          {providerID && zone && (
            <div className="flex items-center gap-3">
              <ActionButton
                busy={busy}
                disabled={!ip || alreadyRight || records.isLoading}
                variant={occupied.length > 0 && !alreadyRight ? "destructive" : "default"}
                onClick={() => (occupied.length > 0 ? setConfirming(true) : write())}
              >
                {occupied.length > 0 ? "Overwrite records" : "Create records"}
              </ActionButton>
              {alreadyRight && (
                <span className="inline-flex items-center gap-1.5 text-xs text-success">
                  <CheckIcon className="size-3.5" />
                  Both records already point here.
                </span>
              )}
              {done && !alreadyRight && (
                <span className="text-xs text-muted-foreground">Written.</span>
              )}
              <span className="flex-1" />
              <span className="font-mono text-[11px] text-subtle-foreground">
                zone {zone.name}
                {shape === "cloudflare" && " · unproxied"}
              </span>
            </div>
          )}
        </CardContent>
      </Card>

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Overwrite ${occupied.length === 1 ? "a record" : `${occupied.length} records`} in ${zone?.name}?`}
        description={
          <>
            Something already answers at{" "}
            {occupied.map((r) => (
              <code key={r.name} className="mr-2">
                {r.name}
              </code>
            ))}
            . Writing replaces every value at those names — whatever is resolving through them now
            stops, and points here instead.
          </>
        }
        confirmWord={domain}
        confirmLabel="Overwrite"
        onConfirm={write}
      />
    </>
  );
}

export const DNSSectionIcon = GlobeIcon;
