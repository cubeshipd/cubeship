"use client";

import { BoxIcon, PlusIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import type { ComponentType } from "react";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { AWSIcon, DigitalOceanIcon } from "@/components/icons";
import { LoadingRows } from "@/components/loading";
import { useOrg } from "@/components/org-context";
import { PageHeader } from "@/components/page-header";
import { SearchableSelect } from "@/components/searchable-select";
import { StatusBadge } from "@/components/status-badge";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  api,
  type RegistryCredential,
  type RegistryProvider,
  type RegistryStatus,
  type Settings,
} from "@/lib/api";
import { message } from "@/lib/errors";

// What each provider is asked for, and why it is not a URL.
//
// The registry a credential is for is its identity, so nothing here has
// a display name: two names for one host is a second thing to keep in
// step for nothing.
const PROVIDERS: Record<
  RegistryProvider,
  { label: string; hint: string; icon: ComponentType<{ className?: string }> }
> = {
  digitalocean: {
    label: "DigitalOcean",
    icon: DigitalOceanIcon,
    hint: "The host never varies — what differs is the registry's name, which is the first path segment of an image.",
  },
  aws: {
    label: "AWS ECR",
    icon: AWSIcon,
    hint: "The registry's address carries your account id and is discovered. What is stored is the access key; the token Docker logs in with is fetched from it at each pull.",
  },
  generic: {
    label: "Other registry",
    icon: BoxIcon,
    hint: "Anything that takes a username and a password: Docker Hub, GitHub, a Harbor you run.",
  },
};

// Logins for registries Cubeship does not run. Cubeship's own registry
// needs none of this — it authenticates each user with their API key.
//
// A table rather than cards: a login has four short facts and no shape
// of its own, and what someone comes here to do is scan a list for one
// of them.
export default function Registries() {
  const { org, loaded } = useOrg();
  const [creds, setCreds] = useState<RegistryCredential[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [statuses, setStatuses] = useState<Record<number, RegistryStatus>>({});
  const router = useRouter();

  // Where a row goes. A login the registry has stopped accepting has
  // nothing to browse — asking for its catalogue would just fail again —
  // so it goes to the one screen that can fix it.
  const open = useCallback(
    (c: RegistryCredential) => {
      if (statuses[c.id]?.state === "unauthorized") {
        router.push(`/registries/settings?id=${c.id}&host=${encodeURIComponent(c.host)}`);
        return;
      }
      router.push(`/registries/detail?id=${c.id}&host=${encodeURIComponent(c.host)}`);
    },
    [router, statuses],
  );

  useEffect(() => {
    api
      .get<Settings>("/settings")
      .then(setSettings)
      .catch(() => setSettings(null));
  }, []);

  const path = org ? `/orgs/${org}/registries` : "";
  const reload = useCallback(() => {
    if (!path) {
      setCreds(null);
      return;
    }
    api
      .get<RegistryCredential[]>(path)
      .then(setCreds)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // One probe per row, in parallel, after the list is on screen. Each is
  // a live round trip to someone else's registry, so waiting for all of
  // them before drawing anything would make the page as slow as the
  // slowest registry in it.
  useEffect(() => {
    if (!org || !creds) return;
    for (const c of creds) {
      api
        .get<RegistryStatus>(`/orgs/${org}/registries/${c.id}/status`)
        .then((s) => setStatuses((prev) => ({ ...prev, [c.id]: s })))
        .catch((e) =>
          setStatuses((prev) => ({
            ...prev,
            [c.id]: { state: "unreachable", detail: message(e) },
          })),
        );
    }
  }, [org, creds]);

  return (
    <>
      <PageHeader
        title="Registries"
        sub={
          org ? (
            <>
              Logins <code className="text-foreground">{org}</code> holds for registries Cubeship
              does not run. An app with an external image pulls through whichever of these matches
              its registry.
            </>
          ) : (
            "Logins for registries Cubeship does not run."
          )
        }
        actions={
          org && (
            <Button onClick={() => setAdding(true)}>
              <PlusIcon />
              New registry
            </Button>
          )
        }
      />

      <ErrorAlert error={error} />

      {loaded && !org && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            No organization selected. A login belongs to one — pick or create an organization from
            the switcher at the top of the sidebar.
          </CardContent>
        </Card>
      )}

      {org && (
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="px-4">Registry</TableHead>
                <TableHead className="px-4">Provider</TableHead>
                <TableHead className="px-4">Region</TableHead>
                <TableHead className="px-4">Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {/* Cubeship's own, always, and always first. It is not a
                  credential — each user authenticates with their own API
                  key — so there is nothing here to add or replace. */}
              <TableRow
                className="cursor-pointer select-none"
                onClick={() => router.push("/registries/detail")}
              >
                <TableCell className="px-4 py-2.5 font-mono text-xs">
                  {settings?.registry_host ?? "not reachable until a domain is set"}
                </TableCell>
                <TableCell className="px-4 py-2.5">
                  <span className="inline-flex items-center gap-2 text-sm">
                    <BoxIcon className="size-4 shrink-0 text-primary" />
                    Cubeship
                  </span>
                </TableCell>
                <TableCell className="px-4 py-2.5 text-xs text-muted-foreground">—</TableCell>
                <TableCell className="px-4 py-2.5">
                  <StatusBadge value="running" />
                </TableCell>
              </TableRow>

              {creds === null && <LoadingRows rows={2} columns={4} />}

              {creds?.length === 0 && (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={4} className="px-4 py-3 text-sm text-muted-foreground">
                    No other registries. Public images need none — add one when a registry refuses
                    an anonymous pull.
                  </TableCell>
                </TableRow>
              )}

              {creds?.map((c) => (
                <TableRow key={c.id} className="cursor-pointer select-none" onClick={() => open(c)}>
                  <TableCell className="px-4 py-2.5 font-mono text-xs">
                    {c.host}
                    {c.namespace && <span className="text-muted-foreground">/{c.namespace}</span>}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 text-sm">
                    <span className="inline-flex items-center gap-2">
                      {(() => {
                        const Icon = PROVIDERS[c.provider]?.icon ?? BoxIcon;
                        return <Icon className="size-4 shrink-0" />;
                      })()}
                      {PROVIDERS[c.provider]?.label ?? c.provider}
                    </span>
                  </TableCell>
                  <TableCell className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                    {c.region || "—"}
                  </TableCell>
                  <TableCell className="px-4 py-2.5" title={statuses[c.id]?.detail}>
                    <StatusBadge value={statuses[c.id]?.state ?? "checking"} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}

      {org && (
        <NewRegistryDialog path={path} open={adding} onOpenChange={setAdding} onCreated={reload} />
      )}
    </>
  );
}

function NewRegistryDialog({
  path,
  open,
  onOpenChange,
  onCreated,
}: {
  path: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: () => void;
}) {
  const [provider, setProvider] = useState<RegistryProvider>("digitalocean");
  const [host, setHost] = useState("");
  const [namespace, setNamespace] = useState("");
  const [region, setRegion] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Only what this provider asks for is sent. The daemon fills in the
  // rest — DigitalOcean's host is fixed, and AWS's is discovered by the
  // same call that proves the key works.
  const complete =
    username &&
    password &&
    (provider === "generic" ? host : provider === "digitalocean" ? namespace : region);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(path, { provider, host, namespace, region, username, password });
      setHost("");
      setNamespace("");
      setRegion("");
      setUsername("");
      setPassword("");
      onCreated();
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>New registry</DialogTitle>
            <DialogDescription>{PROVIDERS[provider].hint}</DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-5">
            <ErrorAlert error={error} className="mb-0" />

            <SearchableSelect
              label="Provider"
              choices={(Object.keys(PROVIDERS) as RegistryProvider[]).map((id) => ({
                value: id,
                label: PROVIDERS[id].label,
                icon: PROVIDERS[id].icon,
              }))}
              value={provider}
              onChange={(v) => setProvider(v as RegistryProvider)}
            />

            {provider === "generic" && (
              <TextField
                label="Registry"
                hint="docker.io for the Hub."
                spellCheck={false}
                value={host}
                onChange={(e) => setHost(e.target.value)}
                placeholder="ghcr.io"
              />
            )}

            {provider === "digitalocean" && (
              <TextField
                label="Registry name"
                hint="What follows registry.digitalocean.com/ in an image path."
                spellCheck={false}
                value={namespace}
                onChange={(e) => setNamespace(e.target.value)}
                placeholder="acme"
              />
            )}

            {provider === "aws" && (
              <TextField
                label="Region"
                hint="Where the ECR registry lives. The account id is discovered."
                spellCheck={false}
                value={region}
                onChange={(e) => setRegion(e.target.value)}
                placeholder="us-east-1"
              />
            )}

            <div className="grid gap-4 sm:grid-cols-2">
              <TextField
                label={provider === "aws" ? "Access key ID" : "Username"}
                hint={provider === "digitalocean" ? "Your DigitalOcean account email." : undefined}
                spellCheck={false}
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
              <TextField
                label={provider === "aws" ? "Secret access key" : "Password or token"}
                hint={
                  provider === "digitalocean"
                    ? "An API key with registry scope."
                    : provider === "generic"
                      ? "An access token wherever the registry offers one."
                      : undefined
                }
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!complete}>
              {provider === "aws" && busy ? "Checking with AWS" : "Add"}
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
