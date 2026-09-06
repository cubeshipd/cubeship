"use client";

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
  type RegistryCredential,
  type RegistryProvider,
  type RegistryStatus,
  type Settings,
} from "@/lib/api";
import { providerIcon } from "@/lib/credentials";
import { message } from "@/lib/errors";

// What a registry asks for beyond the credential it logs in with.
//
// The secret is not in here — that is a credential, stored once and
// usable by anything. What is left is the one thing per provider that
// identifies *which* registry, and it is different in kind at each: a
// host to be typed, a name that is a path segment, a region an address
// is derived from.
const PROVIDERS: Record<RegistryProvider, { label: string; field: string; placeholder: string }> = {
  digitalocean: {
    label: "DigitalOcean",
    field: "Registry name",
    placeholder: "acme",
  },
  aws: {
    label: "AWS ECR",
    field: "Region",
    placeholder: "us-east-1",
  },
  generic: {
    label: "Other registry",
    field: "Registry",
    placeholder: "ghcr.io",
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

  // The credentials, for two things: naming the one each row logs in
  // with, and building the dialog's credential field. One registry does
  // not imply one credential — an AWS key reaching two regions is two
  // rows on one secret — so it is worth a column of its own.
  const loadAccounts = useCallback(() => {
    api
      .get<Credential[]>("/credentials")
      .then(setAccounts)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(loadAccounts, [loadAccounts]);

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
          open={adding}
          onOpenChange={setAdding}
          onCreated={reload}
        />
      }
    </>
  );
}

// The value that means "type a login here instead of picking one". It
// is an option in the credential select rather than a mode switch
// beside it, because it answers the same question: which secret logs
// in. A first registry therefore takes no trip to the Credentials
// screen — a credential is a convenience, not a prerequisite.
const NEW_CREDENTIAL = "";

function NewRegistryDialog({
  path,
  accounts,
  open,
  onOpenChange,
  onCreated,
}: {
  path: string;
  accounts: Credential[];
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: () => void;
}) {
  const [provider, setProvider] = useState<RegistryProvider>("generic");
  const [credentialID, setCredentialID] = useState(NEW_CREDENTIAL);
  const [label, setLabel] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [host, setHost] = useState("");
  const [namespace, setNamespace] = useState("");
  const [region, setRegion] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setError(null);
    setLabel("");
    setUsername("");
    setPassword("");
    setHost("");
    setNamespace("");
    setRegion("");
    // With nothing stored there is nothing to pick, so the form opens
    // on the half that always works.
    setCredentialID(accounts[0]?.id.toString() ?? NEW_CREDENTIAL);
  }, [open, accounts]);

  const typing = credentialID === NEW_CREDENTIAL;
  const shape = PROVIDERS[provider] ?? PROVIDERS.generic;

  // The one thing that identifies which registry, and it differs in
  // kind per provider: a host, a path segment, a region.
  const identified =
    provider === "generic" ? host : provider === "digitalocean" ? namespace : region;
  const setIdentified =
    provider === "generic" ? setHost : provider === "digitalocean" ? setNamespace : setRegion;

  const complete = Boolean(identified) && (typing ? Boolean(password) : true);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(path, {
        provider,
        ...(typing ? { label, username, password } : { credential_id: Number(credentialID) }),
        host,
        namespace,
        region,
      });
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
          </DialogHeader>

          <div className="-mx-2 max-h-[60vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} />

            {/* Always asked, whichever way the login arrives: a
                credential is a secret, not a kind of registry, so a
                stored one cannot answer this. */}
            <SearchableSelect
              label="Provider"
              searchable={false}
              choices={(Object.keys(PROVIDERS) as RegistryProvider[]).map((id) => ({
                value: id,
                label: PROVIDERS[id].label,
                icon: providerIcon(id),
              }))}
              value={provider}
              onChange={(v) => setProvider(v as RegistryProvider)}
            />

            <SearchableSelect
              label="Credential"
              searchable={false}
              choices={[
                ...accounts.map((a) => ({ value: String(a.id), label: a.label })),
                { value: NEW_CREDENTIAL, label: "Type a new one…" },
              ]}
              value={credentialID}
              onChange={setCredentialID}
            />

            {typing && (
              <>
                <TextField
                  label="Label"
                  value={label}
                  spellCheck={false}
                  placeholder="Named after the registry when empty"
                  onChange={(e) => setLabel(e.target.value)}
                />
                <div className="grid gap-4 sm:grid-cols-2">
                  <TextField
                    label="Username or key ID"
                    spellCheck={false}
                    placeholder="Leave empty for a bare token"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                  />
                  <TextField
                    label="Secret"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                </div>
              </>
            )}

            <TextField
              label={shape.field}
              spellCheck={false}
              value={identified}
              placeholder={shape.placeholder}
              onChange={(e) => setIdentified(e.target.value)}
            />
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
