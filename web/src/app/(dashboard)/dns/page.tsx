"use client";

import { PlusIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { CloudflareIcon } from "@/components/icons";
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
import { api, type DNSCredential, type DNSProvider, type DNSStatus } from "@/lib/api";
import { DNS_PROVIDERS } from "@/lib/dns";
import { message } from "@/lib/errors";

// The DNS accounts this organization manages records through.
//
// A table rather than cards: a credential has a label, a provider and a
// state, and what someone comes here to do is scan for one of them.
export default function DNSProviders() {
  const { org, loaded } = useOrg();
  const [creds, setCreds] = useState<DNSCredential[] | null>(null);
  const [statuses, setStatuses] = useState<Record<number, DNSStatus>>({});
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const router = useRouter();

  const path = org ? `/orgs/${org}/dns` : "";
  const reload = useCallback(() => {
    if (!path) {
      setCreds(null);
      return;
    }
    api
      .get<DNSCredential[]>(path)
      .then(setCreds)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // One probe per row, in parallel, after the list is on screen. Each is
  // a live round trip to someone else's API, so waiting for all of them
  // before drawing anything would make the page as slow as the slowest
  // provider in it.
  useEffect(() => {
    if (!org || !creds) return;
    for (const c of creds) {
      api
        .get<DNSStatus>(`/orgs/${org}/dns/${c.id}/status`)
        .then((s) => setStatuses((prev) => ({ ...prev, [c.id]: s })))
        .catch((e) =>
          setStatuses((prev) => ({
            ...prev,
            [c.id]: { state: "unreachable", detail: message(e) },
          })),
        );
    }
  }, [org, creds]);

  const open = useCallback(
    (c: DNSCredential) => {
      // A credential the provider has stopped accepting has no zones
      // to browse — asking would fail the same way — so it goes to the
      // one screen that can fix it.
      const unauthorized = statuses[c.id]?.state === "unauthorized";
      router.push(unauthorized ? `/dns/${c.id}/settings` : `/dns/${c.id}`);
    },
    [router, statuses],
  );

  return (
    <>
      <PageHeader
        title="DNS Providers"
        sub={
          org ? (
            <>
              The DNS accounts <code className="text-foreground">{org}</code> manages records
              through. Cubeship already asks you to point a name at this host — this is where that
              happens, rather than in somebody else&apos;s control panel.
            </>
          ) : (
            "The DNS accounts an organization manages its records through."
          )
        }
        actions={
          org && (
            <Button onClick={() => setAdding(true)}>
              <PlusIcon />
              New provider
            </Button>
          )
        }
      />

      <ErrorAlert error={error} />

      {loaded && !org && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            No organization selected. A DNS credential belongs to one — pick or create an
            organization from the switcher at the top of the sidebar.
          </CardContent>
        </Card>
      )}

      {org && (
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="px-4">Label</TableHead>
                <TableHead className="px-4">Provider</TableHead>
                <TableHead className="px-4">Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {creds === null && <LoadingRows rows={2} columns={3} />}

              {creds?.length === 0 && (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={3} className="px-4 py-3 text-sm text-muted-foreground">
                    No DNS providers yet. Add one to manage where your names point without leaving
                    Cubeship.
                  </TableCell>
                </TableRow>
              )}

              {creds?.map((c) => {
                const Icon = DNS_PROVIDERS[c.provider]?.icon ?? CloudflareIcon;
                return (
                  <TableRow
                    key={c.id}
                    className="cursor-pointer select-none"
                    onClick={() => open(c)}
                  >
                    <TableCell className="px-4 py-2.5 text-sm">{c.label}</TableCell>
                    <TableCell className="px-4 py-2.5 text-sm">
                      <span className="inline-flex items-center gap-2">
                        <Icon className="size-4 shrink-0" />
                        {DNS_PROVIDERS[c.provider]?.label ?? c.provider}
                      </span>
                    </TableCell>
                    {/* The reason is on the badge itself: a row that says
                        unauthorized and nothing else leaves you opening a
                        screen to find out what the provider actually said. */}
                    <TableCell className="px-4 py-2.5" title={statuses[c.id]?.detail}>
                      <StatusBadge value={statuses[c.id]?.state ?? "checking"} />
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Card>
      )}

      {org && (
        <NewProviderDialog path={path} open={adding} onOpenChange={setAdding} onCreated={reload} />
      )}
    </>
  );
}

function NewProviderDialog({
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
  const [provider, setProvider] = useState<DNSProvider>("cloudflare");
  const [label, setLabel] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const shape = DNS_PROVIDERS[provider];

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(path, { provider, label, username, password });
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
            <DialogTitle>New DNS provider</DialogTitle>
            <DialogDescription>
              The account Cubeship reads and writes records through. It belongs to the organization,
              not to one domain.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-5">
            <ErrorAlert error={error} className="mb-0" />

            <SearchableSelect
              label="Provider"
              value={provider}
              onChange={(v) => setProvider(v as DNSProvider)}
              choices={Object.entries(DNS_PROVIDERS).map(([value, p]) => ({
                value,
                label: p.label,
                icon: p.icon,
              }))}
            />

            <TextField
              label="Label"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              hint="What tells this account from another on the same provider. “the Cloudflare one” stops identifying anything the moment there are two."
            />

            {shape.userLabel && (
              <TextField
                label={shape.userLabel}
                spellCheck={false}
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            )}

            <TextField
              label={shape.secretLabel}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              hint={shape.hint}
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton
              type="submit"
              busy={busy}
              disabled={!label || !password || (shape.userLabel !== "" && !username)}
            >
              Add
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
