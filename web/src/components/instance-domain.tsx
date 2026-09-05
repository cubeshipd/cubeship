"use client";

import { useQuery } from "@tanstack/react-query";
import { CheckIcon } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { SearchableSelect } from "@/components/searchable-select";
import { TextField } from "@/components/text-field";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  type App,
  api,
  type DNSCredential,
  type DNSRecord,
  type DNSZone,
  type Settings,
} from "@/lib/api";
import { DNS_PROVIDERS } from "@/lib/dns";
import { message } from "@/lib/errors";

// Giving the instance a name, in one act.
//
// The domain is not typed and then pointed somewhere in a second step:
// with a DNS provider connected, the zone *is* the domain — pick the
// account, pick the zone, and everything else is derivable. The only
// thing nobody can derive is where Let's Encrypt should send expiry
// warnings, so that is the one free field.
//
// One button does the whole thing, in an order that matters: the records
// are written first and the settings saved after. Saving is what brings
// the registry up and starts asking Let's Encrypt for certificates, and
// doing that against a name that does not resolve yet is a wait with a
// failure at the end of it.
//
// The manual path is the same screen with the provider left unset: the
// two records are shown to be copied somewhere else, and the domain
// becomes a field you type.
const DEFAULT_SUBDOMAIN = "cubeship";

export function InstanceDomain({
  settings,
  onSaved,
}: {
  settings: Settings;
  onSaved: (s: Settings) => void;
}) {
  const [providerID, setProviderID] = useState(settings.dns_provider_id ?? "");
  const [zoneID, setZoneID] = useState("");
  // The label in front of the zone. Editable, because one box can hold
  // more than one instance and `cubeship` is only a good default once.
  const [subdomain, setSubdomain] = useState(DEFAULT_SUBDOMAIN);
  const [manualDomain, setManualDomain] = useState(settings.domain);
  const [ip, setIP] = useState(settings.public_ip ?? "");
  const [email, setEmail] = useState(settings.acme_email);

  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const providers = useQuery({
    queryKey: ["dns"],
    queryFn: () => api.get<DNSCredential[]>(`/dns`),
  });

  const zones = useQuery({
    queryKey: ["dns", providerID, "zones"],
    queryFn: () => api.get<DNSZone[]>(`/dns/${providerID}/zones`),
    enabled: Boolean(providerID),
  });

  const zone = zones.data?.find((z) => z.id === zoneID) ?? null;
  const domain = zone ? `${subdomain}.${zone.name}`.replace(/^\./, "") : manualDomain;
  const wildcard = domain ? `*.${domain}` : "";

  // An existing domain is where the form starts: its zone if the
  // credential reaches one, and its own name either way.
  useEffect(() => {
    if (!settings.domain || !zones.data) return;
    const owning = zones.data
      .filter((z) => settings.domain === z.name || settings.domain.endsWith(`.${z.name}`))
      .sort((a, b) => b.name.length - a.name.length)[0];
    if (!owning) return;
    setZoneID(owning.id);
    setSubdomain(
      settings.domain.slice(0, Math.max(0, settings.domain.length - owning.name.length - 1)),
    );
  }, [settings.domain, zones.data]);

  useEffect(() => setIP(settings.public_ip ?? ""), [settings.public_ip]);

  const records = useQuery({
    queryKey: ["dns", providerID, "records", zoneID],
    queryFn: () => api.get<DNSRecord[]>(`/dns/${providerID}/records?zone=${zoneID}`),
    enabled: Boolean(providerID && zoneID),
  });

  // Anything already answering at either name, whatever its type: a
  // CNAME where an A is going is as much in the way as another A, and
  // adding a second answer beside it is how a name starts resolving to
  // two places.
  const occupied = useMemo(
    () => (records.data ?? []).filter((r) => r.name === domain || r.name === wildcard),
    [records.data, domain, wildcard],
  );
  const alreadyRight =
    occupied.length === 2 &&
    occupied.every((r) => r.type === "A" && r.values.length === 1 && r.values[0] === ip);

  async function apply() {
    setBusy(true);
    setError(null);
    try {
      if (providerID && zoneID) {
        for (const name of [domain, wildcard]) {
          await api.put(`/dns/${providerID}/records?zone=${zoneID}`, {
            name,
            type: "A",
            values: [ip],
            ttl: 300,
          });
        }
        await records.refetch();
      }
      const saved = await api.put<Settings>("/settings", {
        domain,
        acme_email: email,
        public_ip: ip,
        dns_provider_id: providerID,
      });
      onSaved(saved);

      // The note about redeploying is worth saying exactly once: on the
      // save that turns certificates on, to someone who already has
      // something running. Traefik routes by the labels a container was
      // created with and Docker cannot change those afterwards, so an
      // app that came up before this stays on HTTP until it is deployed
      // again. On a fresh instance there is nothing standing to warn
      // about, and on a re-save nothing changed — so the running apps
      // are asked for rather than assumed.
      //
      // A toast rather than a line beside the button: what it says is
      // about apps on the rest of the instance, not about this form.
      const running =
        !settings.tls_enabled &&
        saved.tls_enabled &&
        (await api.get<App[]>("/apps").catch(() => [] as App[])).some(
          (a) => a.status === "running",
        );
      if (running) {
        toast.success("Certificates are on", {
          description:
            "Apps already running keep the routing they were deployed with — redeploy them to serve over HTTPS.",
        });
      } else {
        toast.success("Domain set up");
      }
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
    setConfirming(false);
  }

  const automatic = Boolean(providerID);
  const ready = Boolean(domain && (!automatic || (zoneID && ip)));

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
          <ErrorAlert error={error} className="mb-0" />

          <div className="grid grid-cols-12 items-start gap-4">
            <SearchableSelect
              label="DNS provider"
              fieldClassName="col-span-6"
              placeholder="My DNS is elsewhere"
              empty="No DNS providers connected yet."
              value={providerID}
              onChange={(v) => {
                setProviderID(v);
                setZoneID("");
              }}
              busy={providers.isLoading}
              choices={(providers.data ?? []).map((p) => ({
                value: String(p.id),
                label: p.label,
                hint: DNS_PROVIDERS[p.provider]?.label,
                icon: DNS_PROVIDERS[p.provider]?.icon,
              }))}
            />

            {automatic ? (
              <SearchableSelect
                label="Zone"
                fieldClassName="col-span-6"
                placeholder="Pick a domain"
                empty="This credential reaches no zones."
                value={zoneID}
                onChange={setZoneID}
                busy={zones.isLoading}
                choices={(zones.data ?? []).map((z) => ({ value: z.id, label: z.name }))}
              />
            ) : (
              <TextField
                label="Domain"
                fieldClassName="col-span-6"
                spellCheck={false}
                placeholder="cubeship.example.com"
                value={manualDomain}
                onChange={(e) => setManualDomain(e.target.value)}
              />
            )}

            {/* The subdomain only exists in the automatic path: with a
                zone chosen, the rest of the name is the one thing left
                to say, and it is one word rather than a whole domain to
                mistype. */}
            {automatic && zone && (
              <div className="col-span-6 space-y-2">
                <Label className="text-xs text-muted-foreground">Name</Label>
                <div className="flex items-center gap-2">
                  <Input
                    aria-label="Subdomain"
                    className="h-10 flex-1 px-3 text-sm"
                    spellCheck={false}
                    value={subdomain}
                    onChange={(e) => setSubdomain(e.target.value)}
                  />
                  <span className="shrink-0 font-mono text-sm text-muted-foreground">
                    .{zone.name}
                  </span>
                </div>
              </div>
            )}

            {automatic && (
              <TextField
                label="Points at"
                fieldClassName="col-span-6"
                spellCheck={false}
                value={ip}
                onChange={(e) => setIP(e.target.value)}
                hint={
                  settings.public_ip_configured
                    ? "Set by you."
                    : "The address you reached this dashboard at. Correct it if that is not how the world reaches this box."
                }
              />
            )}

            <TextField
              label="Let's Encrypt contact address"
              fieldClassName="col-span-12"
              spellCheck={false}
              placeholder="you@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              hint="Optional. Let's Encrypt registers it with the account; certificates are issued with or without one."
            />
          </div>

          {/* The records, always: as what is about to be written, or as
              what to copy into somebody else's control panel. */}
          {domain && (
            <div className="border border-border">
              {[domain, wildcard].map((name) => {
                const existing = occupied.find((r) => r.name === name);
                const right = existing?.type === "A" && existing.values[0] === ip;
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
                        className={`w-44 shrink-0 truncate text-right text-[11px] ${right ? "text-success" : "text-warning"}`}
                      >
                        now {existing.type} {existing.values.join(", ")}
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
          )}

          {!automatic && (
            <p className="text-xs leading-relaxed text-muted-foreground">
              Create those two yourself, wherever your DNS lives — or{" "}
              <Link href="/dns" className="text-foreground underline underline-offset-4">
                connect a provider
              </Link>{" "}
              and Cubeship writes them for you.
            </p>
          )}

          <div className="flex items-center gap-3">
            <ActionButton
              busy={busy}
              disabled={!ready}
              variant={occupied.length > 0 && !alreadyRight ? "destructive" : "default"}
              onClick={() => (occupied.length > 0 && !alreadyRight ? setConfirming(true) : apply())}
            >
              {occupied.length > 0 && !alreadyRight ? "Overwrite and set up" : "Set up domain"}
            </ActionButton>

            {alreadyRight && (
              <span className="inline-flex items-center gap-1.5 text-xs text-success">
                <CheckIcon className="size-3.5" />
                Both records already point here.
              </span>
            )}
            <span className="flex-1" />
            {automatic && zone && (
              <span className="font-mono text-[11px] text-subtle-foreground">
                zone {zone.name} · unproxied
              </span>
            )}
          </div>
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
            . Writing replaces every value at those names — whatever resolves through them now
            stops, and points here instead.
          </>
        }
        confirmWord={domain}
        confirmLabel="Overwrite"
        onConfirm={apply}
      />
    </>
  );
}
