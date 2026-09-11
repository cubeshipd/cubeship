"use client";

import { useQuery } from "@tanstack/react-query";
import { CheckIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchableSelect } from "@/components/searchable-select";
import { SectionHeader } from "@/components/section-header";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  type App,
  type AppDomain,
  api,
  type DNSProvider,
  type DNSRecord,
  type DNSZone,
  type Settings,
} from "@/lib/api";
import { providerIcon } from "@/lib/credentials";
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
//
// **The list is the screen and adding is a dialog**, which is the shape
// every other list here has. The form used to sit open under the names
// permanently — a DNS provider, a zone, a subdomain and a port, on a
// screen somebody opens to read what an app answers at far more often
// than to add another one. Four empty fields below a list is the list
// saying its real subject is the form.
export function AppNetwork({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [error, setError] = useState<string | null>(null);
  const [removing, setRemoving] = useState<AppDomain | null>(null);
  const [editing, setEditing] = useState<AppDomain | null>(null);
  const [adding, setAdding] = useState(false);
  const [added, setAdded] = useState(false);

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.get<Settings>("/settings"),
  });

  const base = `/apps/${app.reference}`;

  const columns: Column<AppDomain>[] = [
    {
      id: "host",
      header: "Domain",
      width: 58,
      sortBy: (d) => d.host,
      cell: (d) => <span className="font-mono text-xs">{d.host}</span>,
    },
    {
      id: "port",
      header: "Port",
      width: 26,
      sortBy: (d) => d.port || DEFAULT_PORT,
      // A row with no port of its own is served on the default, so that
      // is what it says — the number the container is reached on, not a
      // blank cell somebody has to know the meaning of.
      cell: (d) =>
        d.port ? (
          <span className="font-mono text-xs">{d.port}</span>
        ) : (
          <span className="font-mono text-xs text-muted-foreground" title="This app's default port">
            {DEFAULT_PORT}
          </span>
        ),
    },
    {
      id: "actions",
      header: "",
      width: 16,
      align: "right",
      cell: (d) => (
        <RowActions>
          <RowAction
            icon={PencilIcon}
            label={`Change the port for ${d.host}`}
            onClick={() => setEditing(d)}
          />
          <RowAction
            icon={Trash2Icon}
            label={`Remove ${d.host}`}
            danger
            onClick={() => setRemoving(d)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <SectionHeader
        title="Network"
        sub="Every name this app answers at, and what each one reaches inside the container. A container keeps the routing it was deployed with — redeploy to pick up a change here."
        actions={
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            <PlusIcon />
            Add domain
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={app.domains}
        rowKey={(d) => String(d.id)}
        search={{ placeholder: "Filter domains", by: (d) => [d.host, String(d.port)] }}
        empty="This app answers at no name yet."
      />

      {/* Said here rather than in the dialog, because the dialog closes
          on success and this is the part that is still outstanding: the
          name is routed once the app is deployed again. */}
      {added && (
        <p className="mt-3 inline-flex items-center gap-1.5 text-xs text-success">
          <CheckIcon className="size-3.5" />
          Added. Redeploy to serve it.
        </p>
      )}

      <AddDomainDialog
        base={base}
        open={adding}
        onOpenChange={(v) => {
          setAdding(v);
          if (v) setAdded(false);
        }}
        settings={settings.data}
        // Where the record has to point: this instance, whichever
        // machine the app runs on. Every name arrives here and is
        // routed from here over the cluster's private network, so a
        // record is written once and does not move when an app does.
        address={app.address ?? ""}
        suggested={app.suggested_host ?? ""}
        onSaved={(a) => {
          onSaved(a);
          setAdded(true);
        }}
      />

      <PortDialog
        base={base}
        domain={editing}
        onOpenChange={(v) => !v && setEditing(null)}
        onSaved={onSaved}
      />

      <HealthCheck app={app} onSaved={onSaved} onError={setError} />

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

// The port behind one name, and nothing else.
//
// Its own dialog rather than a field in the row: a table cell with an
// input in it is a control nobody expects to find there, and every other
// list in the dashboard edits a row the same way.
function PortDialog({
  base,
  domain,
  onOpenChange,
  onSaved,
}: {
  base: string;
  // The domain being edited, or null when nothing is.
  domain: AppDomain | null;
  onOpenChange: (v: boolean) => void;
  onSaved: (a: App) => void;
}) {
  const [port, setPort] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!domain) return;
    setPort(domain.port ? String(domain.port) : "");
    setError(null);
  }, [domain]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!domain) return;
    setBusy(true);
    setError(null);
    try {
      onSaved(await api.patch<App>(`${base}/domains/${domain.id}`, { port: Number(port) || 0 }));
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={domain !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>Port for {domain?.host}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <TextField
              label="Port"
              autoFocus
              spellCheck={false}
              placeholder={String(DEFAULT_PORT)}
              value={port}
              onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
              hint={`What this name reaches inside the container. Leave it empty to serve it on ${DEFAULT_PORT}. The app keeps the routing it was deployed with until it is deployed again.`}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy}>
              Save
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// Adding a name, through a DNS provider or by hand.
//
// The provider path writes the record and adds the name in one act, in
// that order: adding a name Cubeship then cannot resolve would be an app
// that says it is served somewhere nothing answers.
function AddDomainDialog({
  base,
  open,
  onOpenChange,
  settings,
  address,
  suggested,
  onSaved,
}: {
  base: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  settings: Settings | undefined;
  // Where a record for this app has to point: this instance's own
  // public address, whichever machine the app runs on. Empty when the
  // instance does not know it — a name nothing can be pointed at yet.
  address: string;
  // A name this app could answer at, under the instance's own domain.
  // The daemon works it out — it is the app's own reference under that
  // domain — so it is offered here rather than composed here. Empty
  // while the instance has no domain to build one under.
  suggested: string;
  onSaved: (a: App) => void;
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
  const [error, setError] = useState<string | null>(null);

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
    queryFn: () => api.get<DNSProvider[]>("/dns"),
    enabled: open,
  });

  const zones = useQuery({
    queryKey: ["dns", providerID, "zones"],
    queryFn: () => api.get<DNSZone[]>(`/dns/${providerID}/zones`),
    enabled: open && automatic,
  });

  const zone = zones.data?.find((z) => z.id === zoneID) ?? null;
  const host = onInstanceDomain
    ? suggested
    : zone
      ? `${subdomain}.${zone.name}`.replace(/^\./, "")
      : manualHost.trim();
  const ip = address;

  const records = useQuery({
    queryKey: ["dns", providerID, "records", zoneID],
    queryFn: () => api.get<DNSRecord[]>(`/dns/${providerID}/records?zone=${zoneID}`),
    enabled: open && automatic && Boolean(zoneID),
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
    setError(null);
    try {
      // The record first. A name added here that does not resolve is an
      // app claiming to be served somewhere nothing answers — which is
      // why the button is disabled without an address rather than this
      // quietly skipping the write.
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
      setConfirming(false);
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
      setConfirming(false);
    }
    setBusy(false);
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>Add domain</DialogTitle>
          </DialogHeader>

          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />

            <div className="grid grid-cols-12 items-start gap-4">
              <SearchableSelect
                label="DNS provider"
                fieldClassName="col-span-12"
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
                    label: p.provider_name,
                    hint: p.label,
                    icon: providerIcon(p.provider),
                  })),
                ]}
              />

              {onInstanceDomain ? (
                <div className="col-span-12 space-y-2">
                  <Label className="text-xs text-muted-foreground">Name</Label>
                  <div className="flex h-10 items-center overflow-x-auto border border-border bg-secondary/40 px-3 font-mono text-sm text-muted-foreground">
                    {suggested}
                  </div>
                </div>
              ) : automatic ? (
                <SearchableSelect
                  label="Zone"
                  fieldClassName="col-span-12"
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
                  fieldClassName="col-span-12"
                  spellCheck={false}
                  placeholder="app.example.com"
                  value={manualHost}
                  onChange={(e) => setManualHost(e.target.value)}
                />
              )}

              {automatic && zone && (
                <div className="col-span-12 space-y-2">
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
                fieldClassName="col-span-4"
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

            {/* No address, no record — and no domain either, rather than a
                name added here that resolves nowhere. It used to write the
                name and skip the record, which reported success for an app
                that answered at nothing. */}
            {automatic && !ip && (
              <Notice tone="warning" className="mb-0">
                This instance does not know its own public address, so there is nothing to point the
                record at. Set it under{" "}
                <Link href="/settings" className="underline underline-offset-4">
                  Settings
                </Link>{" "}
                — it is the address the world reaches this machine at, not one on its private
                network.
              </Notice>
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
                Point that name at{" "}
                {address ? <code className="text-foreground">{address}</code> : "this host"}{" "}
                yourself, wherever your DNS lives — or{" "}
                <Link href="/dns" className="text-foreground underline underline-offset-4">
                  connect a provider
                </Link>{" "}
                and Cubeship writes the record for you.
              </p>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton
              busy={busy}
              disabled={!host || (automatic && (!zoneID || !ip))}
              variant={occupied.length > 0 ? "destructive" : "default"}
              onClick={() => (occupied.length > 0 ? setConfirming(true) : add())}
            >
              <PlusIcon />
              {occupied.length > 0 ? "Overwrite and add" : "Add domain"}
            </ActionButton>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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

// What Traefik asks the app for before it trusts a container with
// traffic.
//
// **Off by default, and that is the only safe default.** A check needs
// a path, the path is something only the app's author knows, and a
// wrong one does not degrade a name by halves — Traefik marks every
// replica down at once and the name answers 503.
//
// What it buys is the case a retry cannot cover. An app on several
// machines already routes around a replica that has *gone*: the edge
// retries against the next one and nobody sees anything. A replica that
// is up and broken answers the connection, so no retry ever fires for
// it, and nothing but a check takes it out.
function HealthCheck({
  app,
  onSaved,
  onError,
}: {
  app: App;
  onSaved: (a: App) => void;
  onError: (e: string | null) => void;
}) {
  const current = app.health_path ?? "";
  const [path, setPath] = useState(current);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const dirty = path !== current;

  return (
    <>
      <SectionHeader
        title="Health check"
        sub="What Traefik asks this app for before sending it traffic. Leave it empty to check nothing, which is what every app starts as."
      />
      <Card>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              onError(null);
              setSaved(false);
              try {
                onSaved(await api.patch<App>(`/apps/${app.reference}`, { health_path: path }));
                setSaved(true);
              } catch (err) {
                onError(message(err));
              }
              setBusy(false);
            }}
          >
            <TextField
              label="Path"
              placeholder="/healthz"
              value={path}
              onChange={(e) => {
                setPath(e.target.value);
                setSaved(false);
              }}
              hint="It has to start with / and hold only what a URL path may — it goes into the proxy's own configuration. Checked every 10 seconds, with 5 seconds to answer."
            />
            {dirty && path !== "" && (
              <Notice tone="warning">
                A path this app does not answer 2xx or 3xx on takes every copy of it out of
                rotation, and {app.domains.length > 0 ? "its names answer" : "it would answer"} 503
                rather than degrading. Check what the app actually serves before saving.
              </Notice>
            )}
            {!dirty && current !== "" && app.nodes.length > 1 && (
              <Notice>
                A copy of this app that stops answering <code>{current}</code> is taken out of the
                balancer and the rest go on serving. One that has gone needs no check — the edge
                retries against the next machine on its own.
              </Notice>
            )}
            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={busy} disabled={!dirty}>
                Save
              </ActionButton>
              {saved && !dirty && (
                <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-success">
                  <CheckIcon className="size-3.5" /> Saved
                </span>
              )}
            </div>
          </form>
        </CardContent>
      </Card>
    </>
  );
}
