"use client";

import { cn } from "cn";
import { BoxIcon, PlusIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingRows } from "@/components/loading";
import { PageHeader } from "@/components/page-header";
import { SearchableSelect } from "@/components/searchable-select";
import { StatusBadge } from "@/components/status-badge";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
  type Credential,
  type CredentialProvider,
  type RegistryCredential,
  type RegistryProvider,
  type RegistryStatus,
  type Settings,
} from "@/lib/api";
import { providerIcon, REGISTRY_CREDENTIALS } from "@/lib/credentials";
import { message } from "@/lib/errors";

// What a registry asks for beyond the account it authenticates as.
//
// The login is not in here any more — that is the credential, stored
// once under Credentials and used by everything on the same provider.
// What is left is the one thing per provider that identifies *which*
// registry, and it is different in kind at each: a host to be typed, a
// name that is a path segment, a region an address is derived from.
const PROVIDERS: Record<RegistryProvider, { label: string; hint: string }> = {
  digitalocean: {
    label: "DigitalOcean",
    hint: "The host never varies — what differs is the registry's name, which is the first path segment of an image.",
  },
  aws: {
    label: "AWS ECR",
    hint: "The registry's address carries your account id and is discovered by the same call that proves the key can read a registry at all.",
  },
  generic: {
    label: "Other registry",
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
  const [creds, setCreds] = useState<RegistryCredential[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [accounts, setAccounts] = useState<Credential[]>([]);
  const [providers, setProviders] = useState<CredentialProvider[]>([]);
  const [statuses, setStatuses] = useState<Record<number, RegistryStatus>>({});
  const router = useRouter();

  // Where a row goes. A login the registry has stopped accepting has
  // nothing to browse — asking for its catalogue would just fail again —
  // so it goes to the one screen that can fix it.
  const open = useCallback(
    (c: RegistryCredential) => {
      const unauthorized = statuses[c.id]?.state === "unauthorized";
      router.push(unauthorized ? `/registries/${c.id}/settings` : `/registries/${c.id}`);
    },
    [router, statuses],
  );

  useEffect(() => {
    api
      .get<Settings>("/settings")
      .then(setSettings)
      .catch(() => setSettings(null));
  }, []);

  // The accounts, for two things: naming the one each row authenticates
  // as, and building the dialog's first field. One registry does not
  // imply one account — an AWS key reaching two regions is two rows on
  // one credential — so the account is worth a column of its own.
  const loadAccounts = useCallback(() => {
    api
      .get<Credential[]>(REGISTRY_CREDENTIALS)
      .then(setAccounts)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(loadAccounts, [loadAccounts]);

  // What each provider's login is called. Served rather than copied,
  // so a provider whose secret is a single token is not asked for a
  // username the account has nowhere to put.
  useEffect(() => {
    api
      .get<CredentialProvider[]>("/credentials/providers")
      .then(setProviders)
      .catch((e) => setError(message(e)));
  }, []);

  const path = `/registries`;
  const reload = useCallback(() => {
    api
      .get<RegistryCredential[]>(path)
      .then(setCreds)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  // One probe per row, in parallel, after the list is on screen. Each is
  // a live round trip to someone else's registry, so waiting for all of
  // them before drawing anything would make the page as slow as the
  // slowest registry in it.
  useEffect(() => {
    if (!creds) return;
    for (const c of creds) {
      api
        .get<RegistryStatus>(`/registries/${c.id}/status`)
        .then((s) => setStatuses((prev) => ({ ...prev, [c.id]: s })))
        .catch((e) =>
          setStatuses((prev) => ({
            ...prev,
            [c.id]: { state: "unreachable", detail: message(e) },
          })),
        );
    }
  }, [creds]);

  return (
    <>
      <PageHeader
        title="Registries"
        sub={
          <>
            Logins this instance holds for registries Cubeship does not run. An app with an external
            image pulls through whichever of these matches its registry.
          </>
        }
        actions={
          <Button onClick={() => setAdding(true)}>
            <PlusIcon />
            New registry
          </Button>
        }
      />

      <ErrorAlert error={error} />

      {
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="px-4">Registry</TableHead>
                <TableHead className="px-4">Provider</TableHead>
                <TableHead className="px-4">Credential</TableHead>
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
                onClick={() => router.push("/registries/cubeship")}
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
                {/* Cubeship's own takes no credential and lives in no
                    region: each user authenticates with their own API
                    key. */}
                <TableCell className="px-4 py-2.5 text-xs text-muted-foreground">—</TableCell>
                <TableCell className="px-4 py-2.5 text-xs text-muted-foreground">—</TableCell>
                <TableCell className="px-4 py-2.5">
                  <StatusBadge value="running" />
                </TableCell>
              </TableRow>

              {creds === null && <LoadingRows rows={2} columns={5} />}

              {creds?.length === 0 && (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={5} className="px-4 py-3 text-sm text-muted-foreground">
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
                        const Icon = providerIcon(c.provider);
                        return <Icon className="size-4 shrink-0" />;
                      })()}
                      {PROVIDERS[c.provider]?.label ?? c.provider}
                    </span>
                  </TableCell>
                  <TableCell className="px-4 py-2.5 text-sm">
                    {accounts.find((a) => a.id === c.credential_id)?.label ?? "—"}
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
      }

      {
        <NewRegistryDialog
          path={path}
          accounts={accounts}
          providers={providers}
          open={adding}
          onOpenChange={setAdding}
          onCreated={reload}
        />
      }
    </>
  );
}

function NewRegistryDialog({
  path,
  accounts,
  providers,
  open,
  onOpenChange,
  onCreated,
}: {
  path: string;
  accounts: Credential[];
  providers: CredentialProvider[];
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: () => void;
}) {
  // Two ways in, and neither is privileged over the other: pick an
  // account this instance already holds, or type a login and let the
  // account be made from it. A stored account is a convenience — the
  // second registry on one DigitalOcean token, or an AWS key already
  // there for Route 53 — not a thing to go and create first.
  const [mode, setMode] = useState<"saved" | "new">("new");
  const [credentialID, setCredentialID] = useState("");
  const [provider, setProvider] = useState<RegistryProvider>("digitalocean");
  const [label, setLabel] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [host, setHost] = useState("");
  const [namespace, setNamespace] = useState("");
  const [region, setRegion] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // With nothing stored there is nothing to pick, so the form opens on
  // the half that always works.
  useEffect(() => {
    if (!open) return;
    setError(null);
    setMode(accounts.length > 0 ? "saved" : "new");
  }, [open, accounts.length]);

  const account = accounts.find((a) => String(a.id) === credentialID);
  const saved = mode === "saved";
  const chosen: RegistryProvider = saved
    ? ((account?.provider ?? "generic") as RegistryProvider)
    : provider;
  const shape = PROVIDERS[chosen] ?? PROVIDERS.generic;
  // Which registry to ask about follows from the provider, and under
  // "saved account" nothing knows it until an account is picked. Asking
  // for a host before then is asking a question whose answer may turn
  // out not to apply.
  const known = !saved || account !== undefined;

  // What the login's two halves are called is the daemon's answer, not
  // a copy kept here: a provider whose secret is one value has no first
  // field, and asking for one would be asking for something that does
  // not exist.
  const spec = providers.find((p) => p.provider === chosen);
  const needsUsername = !!spec?.username_label;

  const identified = chosen === "generic" ? host : chosen === "digitalocean" ? namespace : region;
  const complete = saved
    ? credentialID && identified
    : identified && password && (!needsUsername || username);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(path, {
        ...(saved
          ? { credential_id: Number(credentialID) }
          : {
              provider,
              label,
              ...(needsUsername ? { username } : {}),
              password,
            }),
        host,
        namespace,
        region,
      });
      setHost("");
      setNamespace("");
      setRegion("");
      setLabel("");
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
            <DialogDescription>{shape.hint}</DialogDescription>
          </DialogHeader>

          <div className="-mx-2 max-h-[60vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} />

            {accounts.length > 0 && (
              <div className="flex items-center gap-1">
                {(
                  [
                    ["saved", "Saved account"],
                    ["new", "New login"],
                  ] as const
                ).map(([value, text]) => (
                  <Button
                    key={value}
                    type="button"
                    variant="ghost"
                    size="xs"
                    aria-pressed={mode === value}
                    onClick={() => setMode(value)}
                    className={cn(mode === value && "bg-secondary text-foreground")}
                  >
                    {text}
                  </Button>
                ))}
              </div>
            )}

            {saved ? (
              <SearchableSelect
                label="Credential"
                placeholder="Which account logs in"
                choices={accounts.map((a) => ({
                  value: String(a.id),
                  label: a.label,
                  hint: a.provider_name,
                  icon: providerIcon(a.provider),
                }))}
                value={credentialID}
                onChange={setCredentialID}
                hint="Its secret stays where it is. Rotating it later is one edit, and every registry on it follows."
              />
            ) : (
              <>
                <SearchableSelect
                  label="Provider"
                  choices={(Object.keys(PROVIDERS) as RegistryProvider[]).map((id) => ({
                    value: id,
                    label: PROVIDERS[id].label,
                    icon: providerIcon(id),
                  }))}
                  value={provider}
                  onChange={(v) => setProvider(v as RegistryProvider)}
                />

                {/* Side by side only when there are two halves. A
                    provider whose secret is a single token would
                    otherwise get a half-width box and an empty column
                    beside it. */}
                <div className={cn("grid gap-4", needsUsername && "sm:grid-cols-2")}>
                  {needsUsername && (
                    <TextField
                      label={spec?.username_label ?? "Username"}
                      spellCheck={false}
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                    />
                  )}
                  <TextField
                    label={spec?.password_label ?? "Password or token"}
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    hint={needsUsername ? undefined : spec?.hint}
                  />
                </div>

                <TextField
                  label="Account name"
                  value={label}
                  spellCheck={false}
                  onChange={(e) => setLabel(e.target.value)}
                  hint="What this login is called under Credentials, where it is stored and can be picked again. Leave it empty and it is named after the registry."
                />
              </>
            )}

            {known && chosen === "generic" && (
              <TextField
                label="Registry"
                hint="docker.io for the Hub."
                spellCheck={false}
                value={host}
                onChange={(e) => setHost(e.target.value)}
                placeholder="ghcr.io"
              />
            )}

            {known && chosen === "digitalocean" && (
              <TextField
                label="Registry name"
                hint="What follows registry.digitalocean.com/ in an image path."
                spellCheck={false}
                value={namespace}
                onChange={(e) => setNamespace(e.target.value)}
                placeholder="acme"
              />
            )}

            {known && chosen === "aws" && (
              <TextField
                label="Region"
                hint="Where the ECR registry lives. The account id is discovered."
                spellCheck={false}
                value={region}
                onChange={(e) => setRegion(e.target.value)}
                placeholder="us-east-1"
              />
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!complete}>
              {chosen === "aws" && busy ? "Checking with AWS" : "Add"}
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
