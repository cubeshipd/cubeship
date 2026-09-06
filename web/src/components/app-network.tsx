"use client";

import { useQuery } from "@tanstack/react-query";
import { CheckIcon, PlusIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { SectionHeader } from "@/components/page-header";
import { SearchableSelect } from "@/components/searchable-select";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  type App,
  type AppDomain,
  api,
  type DNSCredential,
  type DNSRecord,
  type DNSZone,
  type Settings,
} from "@/lib/api";
import { DNS_CREDENTIALS, providerIcon } from "@/lib/credentials";
import { message } from "@/lib/errors";

// DEFAULT_PORT is what the daemon serves a name on when nobody said
// otherwise. It is app.DefaultPort.
const DEFAULT_PORT = 8080;

// INSTANCE_DOMAIN is the third answer to "where does this name live",
// beside a stored DNS credential and "somewhere Cubeship cannot reach":
// under the instance's own domain, which is where the dashboard itself
// answers. It is not a credential and writes no record — on the sslip.io
// address a default install takes, every name under it already resolves
// to this host.
const INSTANCE_DOMAIN = "instance";

// Where an app answers, and on which port.
//
// The same act as giving the instance its own name — pick the provider,
// pick the zone, and the record is written — because it is the same act.
// The only thing that differs is what the name points at, and that is
// the same address either way: every app runs on this host.
//
// Each name carries its own port, and that is the whole reason this is a
// list rather than a field. An image can expose several; api.example.com
// and admin.example.com on one container are two of them.
export function AppNetwork({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [error, setError] = useState<string | null>(null);
  const [removing, setRemoving] = useState<AppDomain | null>(null);

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.get<Settings>("/settings"),
  });

  const base = `/apps/${app.reference}`;

  return (
    <>
      <SectionHeader
        title="Network"
        sub="Every name this app answers at, and what each one reaches inside the container. A container keeps the routing it was deployed with — redeploy to pick up a change here."
      />

      <ErrorAlert error={error} />

      {app.domains.length > 0 && (
        <Card className="mb-4 py-0">
          <div className="divide-y divide-border">
            {app.domains.map((d) => (
              <DomainRow
                key={d.id}
                base={base}
                domain={d}
                onSaved={onSaved}
                onError={setError}
                onRemove={() => setRemoving(d)}
              />
            ))}
          </div>
        </Card>
      )}

      <AddDomain
        base={base}
        settings={settings.data}
        suggested={app.suggested_host ?? ""}
        onSaved={onSaved}
        onError={setError}
      />

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(v) => !v && setRemoving(null)}
        title={`Stop serving ${removing?.host}?`}
        confirmWord={removing?.host}
        description="The DNS record stays where it is — this only stops Cubeship routing that name here. The container keeps the routing it was deployed with, so it goes on answering until the app is redeployed."
        confirmLabel="Remove"
        onConfirm={async () => {
          if (!removing) return;
          onSaved(await api.del<App>(`${base}/domains/${removing.id}`));
          setRemoving(null);
        }}
      />
    </>
  );
}

// One name, and the port behind it.
function DomainRow({
  base,
  domain,
  onSaved,
  onError,
  onRemove,
}: {
  base: string;
  domain: AppDomain;
  onSaved: (a: App) => void;
  onError: (e: string | null) => void;
  onRemove: () => void;
}) {
  const [port, setPort] = useState(domain.port ? String(domain.port) : "");
  const [busy, setBusy] = useState(false);
  const dirty = (Number(port) || 0) !== domain.port;

  async function save() {
    setBusy(true);
    onError(null);
    try {
      onSaved(await api.patch<App>(`${base}/domains/${domain.id}`, { port: Number(port) || 0 }));
    } catch (err) {
      onError(message(err));
    }
    setBusy(false);
  }

  return (
    <div className="flex items-center gap-4 px-4 py-3">
      <span className="min-w-0 flex-1 truncate font-mono text-xs">{domain.host}</span>

      <div className="flex shrink-0 items-center gap-2">
        <Input
          aria-label={`Port for ${domain.host}`}
          className="h-9 w-28 px-3 text-sm"
          placeholder="8080"
          spellCheck={false}
          value={port}
          onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
        />
        {dirty && (
          <ActionButton size="sm" busy={busy} onClick={save}>
            Save
          </ActionButton>
        )}
        <Button
          variant="ghost"
          size="xs"
          aria-label={`Remove ${domain.host}`}
          className="size-8 p-0 text-muted-foreground hover:text-destructive"
          onClick={onRemove}
        >
          <Trash2Icon className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}

// Adding a name, through a DNS provider or by hand.
//
// The provider path writes the record and adds the name in one act, in
// that order: adding a name Cubeship then cannot resolve would be an app
// that says it is served somewhere nothing answers.
function AddDomain({
  base,
  settings,
  suggested,
  onSaved,
  onError,
}: {
  base: string;
  settings: Settings | undefined;
  // A name this app could answer at, under the instance's own domain.
  // The daemon works it out — it is the app's own reference under that
  // domain — so it is offered here rather than composed here. Empty
  // while the instance has no domain to build one under.
  suggested: string;
  onSaved: (a: App) => void;
  onError: (e: string | null) => void;
}) {
  const [providerID, setProviderID] = useState("");
  const [zoneID, setZoneID] = useState("");
  const [subdomain, setSubdomain] = useState("");
  const [manualHost, setManualHost] = useState("");
  // The port is asked for, not detected. 8080 is where most things
  // listen, so it is the value the field opens on rather than a
  // placeholder somebody has to accept.
  const [port, setPort] = useState(String(DEFAULT_PORT));
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  // The instance already knows which provider writes its own records and
  // what its address is. An app is on the same host, so both are the
  // right starting point rather than a second thing to answer.
  useEffect(() => {
    if (settings?.dns_provider_id) {
      setProviderID(settings.dns_provider_id);
      return;
    }
    // Nothing connected, but the instance has a domain and a name for
    // this app under it. That is the shortest path from an app that
    // answers nowhere to one that answers somewhere, so it is where the
    // form opens.
    if (suggested) setProviderID(INSTANCE_DOMAIN);
  }, [settings?.dns_provider_id, suggested]);

  const onInstanceDomain = providerID === INSTANCE_DOMAIN;
  // automatic is the path that writes a record before adding the name.
  // The instance's own domain needs none: under a wildcard address the
  // name already resolves, and under a real one it is the operator's
  // wildcard record that answers.
  const automatic = Boolean(providerID) && !onInstanceDomain;

  const providers = useQuery({
    queryKey: ["dns"],
    queryFn: () => api.get<DNSCredential[]>(DNS_CREDENTIALS),
  });

  const zones = useQuery({
    queryKey: ["dns", providerID, "zones"],
    queryFn: () => api.get<DNSZone[]>(`/dns/${providerID}/zones`),
    enabled: automatic,
  });

  const zone = zones.data?.find((z) => z.id === zoneID) ?? null;
  const host = onInstanceDomain
    ? suggested
    : zone
      ? `${subdomain}.${zone.name}`.replace(/^\./, "")
      : manualHost.trim();
  const ip = settings?.public_ip ?? "";

  const records = useQuery({
    queryKey: ["dns", providerID, "records", zoneID],
    queryFn: () => api.get<DNSRecord[]>(`/dns/${providerID}/records?zone=${zoneID}`),
    enabled: automatic && Boolean(zoneID),
  });

  // Anything already answering at that name, of any type: a CNAME where
  // an A is going is as much in the way as another A.
  const occupied = useMemo(
    () => (records.data ?? []).filter((r) => r.name === host),
    [records.data, host],
  );
  const [confirming, setConfirming] = useState(false);

  async function add() {
    setBusy(true);
    onError(null);
    setDone(false);
    try {
      // The record first. A name added here that does not resolve is an
      // app claiming to be served somewhere nothing answers.
      if (automatic && zone && ip) {
        await api.put(`/dns/${providerID}/records?zone=${zoneID}`, {
          name: host,
          type: "A",
          values: [ip],
          ttl: 300,
        });
        await records.refetch();
      }
      onSaved(await api.post<App>(`${base}/domains`, { host, port: Number(port) || 0 }));
      setSubdomain("");
      setManualHost("");
      setPort(String(DEFAULT_PORT));
      setDone(true);
    } catch (err) {
      onError(message(err));
    }
    setBusy(false);
    setConfirming(false);
  }

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
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
              choices={[
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
                  label: p.label,
                  hint: p.provider_name,
                  icon: providerIcon(p.provider),
                })),
              ]}
            />

            {onInstanceDomain ? (
              <div className="col-span-6 space-y-2">
                <Label className="text-xs text-muted-foreground">Name</Label>
                <div className="flex h-10 items-center overflow-x-auto border border-border bg-secondary/40 px-3 font-mono text-sm text-muted-foreground">
                  {suggested}
                </div>
              </div>
            ) : automatic ? (
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
                placeholder="app.example.com"
                value={manualHost}
                onChange={(e) => setManualHost(e.target.value)}
              />
            )}

            {automatic && zone && (
              <div className="col-span-6 space-y-2">
                <Label className="text-xs text-muted-foreground">Name</Label>
                <div className="flex items-center gap-2">
                  <Input
                    aria-label="Subdomain"
                    className="h-10 flex-1 px-3 text-sm"
                    spellCheck={false}
                    placeholder="app"
                    value={subdomain}
                    onChange={(e) => setSubdomain(e.target.value)}
                  />
                  <span className="shrink-0 font-mono text-sm text-muted-foreground">
                    .{zone.name}
                  </span>
                </div>
              </div>
            )}

            <TextField
              label="Port"
              fieldClassName="col-span-3"
              spellCheck={false}
              placeholder="8080"
              value={port}
              onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
            />
          </div>

          {host && !onInstanceDomain && (
            <div className="border border-border px-3 py-2 font-mono text-xs">
              <span className="w-8 shrink-0 text-muted-foreground">A</span>
              <span className="ml-4">{host}</span>
              <span className="ml-4 text-muted-foreground">{ip || "—"}</span>
              {occupied.length > 0 && (
                <span className="ml-4 text-warning">
                  now {occupied[0].type} {occupied[0].values.join(", ")}
                </span>
              )}
            </div>
          )}

          {onInstanceDomain && (
            <p className="text-xs leading-relaxed text-muted-foreground">
              {settings?.wildcard_domain
                ? "Every name under this instance's domain already resolves to this host, so there is no record to write and nothing to wait for."
                : "This needs a wildcard record for the instance's domain pointing at this host. Without one the name will not resolve, whatever is added here."}
            </p>
          )}

          {!automatic && !onInstanceDomain && (
            <p className="text-xs leading-relaxed text-muted-foreground">
              Point that name at this host yourself, wherever your DNS lives — or{" "}
              <Link href="/dns" className="text-foreground underline underline-offset-4">
                connect a provider
              </Link>{" "}
              and Cubeship writes the record for you.
            </p>
          )}

          <div className="flex items-center gap-3">
            <ActionButton
              busy={busy}
              disabled={!host || (automatic && !zoneID)}
              variant={occupied.length > 0 ? "destructive" : "default"}
              onClick={() => (occupied.length > 0 ? setConfirming(true) : add())}
            >
              <PlusIcon />
              {occupied.length > 0 ? "Overwrite and add" : "Add domain"}
            </ActionButton>
            {done && (
              <span className="inline-flex items-center gap-1.5 text-xs text-success">
                <CheckIcon className="size-3.5" />
                Added. Redeploy to serve it.
              </span>
            )}
          </div>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Overwrite the record for ${host}?`}
        confirmWord={host}
        description="Something already answers at that name. Writing replaces every value — whatever resolves through it now stops, and points at this host instead."
        confirmLabel="Overwrite"
        onConfirm={add}
      />
    </>
  );
}
