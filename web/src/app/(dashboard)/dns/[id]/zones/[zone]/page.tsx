"use client";

import { ChevronLeftIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingRows } from "@/components/loading";
import { Notice } from "@/components/notice";
import { useOrg } from "@/components/org-context";
import { PageHeader } from "@/components/page-header";
import { SearchBar } from "@/components/search-bar";
import { SearchableSelect } from "@/components/searchable-select";
import { TextAreaField, TextField } from "@/components/text-field";
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
import { api, type DNSCredential, type DNSRecord, type DNSZone } from "@/lib/api";
import { RECORD_TYPES } from "@/lib/dns";
import { message } from "@/lib/errors";

// One zone's records.
//
// The zone travels in the path as its **name**, not as the provider's
// id: a name is what someone recognises in a link they were sent, and it
// is unique within an account either way. The id is what every call
// needs, so it is resolved from the zone listing on arrival — one extra
// request, in exchange for a URL that says what it is.
export default function ZoneRecords({ params }: { params: Promise<{ id: string; zone: string }> }) {
  const { id, zone: zoneName } = use(params);
  const name = decodeURIComponent(zoneName);
  const { org } = useOrg();

  const [credential, setCredential] = useState<DNSCredential | null>(null);
  const [zone, setZone] = useState<DNSZone | null>(null);
  const [missing, setMissing] = useState(false);
  const [records, setRecords] = useState<DNSRecord[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  const [editing, setEditing] = useState<{ record?: DNSRecord } | null>(null);
  const [deleting, setDeleting] = useState<DNSRecord | null>(null);

  const base = org ? `/orgs/${org}/dns/${id}` : "";

  useEffect(() => {
    if (!org) return;
    api
      .get<DNSCredential[]>(`/orgs/${org}/dns`)
      .then((list) => setCredential(list.find((c) => String(c.id) === id) ?? null))
      .catch(() => setCredential(null));
  }, [org, id]);

  // The name in the path is resolved against what the credential can
  // actually reach, so a link to a zone this credential lost access to
  // says so rather than failing on the first record call.
  useEffect(() => {
    if (!base) return;
    api
      .get<DNSZone[]>(`${base}/zones`)
      .then((list) => {
        const found = list.find((z) => z.name === name) ?? null;
        setZone(found);
        setMissing(found === null);
      })
      .catch((e) => setError(message(e)));
  }, [base, name]);

  const load = useCallback(() => {
    if (!base || !zone) return;
    setError(null);
    api
      .get<DNSRecord[]>(`${base}/records?zone=${encodeURIComponent(zone.id)}`)
      .then(setRecords)
      .catch((e) => setError(message(e)));
  }, [base, zone]);
  useEffect(load, [load]);

  const q = query.trim().toLowerCase();
  const filtered = (records ?? []).filter(
    (r) =>
      r.name.toLowerCase().includes(q) ||
      r.type.toLowerCase().includes(q) ||
      r.values.some((v) => v.toLowerCase().includes(q)),
  );

  return (
    <>
      <Link
        href={`/dns/${id}`}
        className="mb-4 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      >
        <ChevronLeftIcon className="size-3.5" />
        {credential?.label || "Zones"}
      </Link>

      <PageHeader
        title={name}
        literal
        sub="Every record in this zone, as the provider reports it."
        actions={
          zone && (
            <Button onClick={() => setEditing({})}>
              <PlusIcon />
              New record
            </Button>
          )
        }
        below={
          records &&
          records.length > 0 && (
            <SearchBar
              value={query}
              onChange={setQuery}
              placeholder="Filter by name, type or value"
              trailing={
                <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
                  {filtered.length}/{records.length}
                </span>
              }
            />
          )
        }
      />

      <ErrorAlert error={error} />

      {missing && (
        <Notice tone="warning">
          This credential does not reach <code>{name}</code>. Either the zone is gone, or the token
          was scoped to a different one.
        </Notice>
      )}

      {records && records.length > 0 && filtered.length === 0 && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            Nothing matches “{query}”.
          </CardContent>
        </Card>
      )}

      {!missing && (records === null || filtered.length > 0) && (
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="px-4">Type</TableHead>
                <TableHead className="px-4">Name</TableHead>
                <TableHead className="px-4">Value</TableHead>
                <TableHead className="px-4">TTL</TableHead>
                <TableHead className="px-4" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {records === null && <LoadingRows rows={6} columns={5} />}

              {filtered.map((record) => (
                <TableRow key={`${record.name}-${record.type}`} className="select-none">
                  <TableCell className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                    {record.type}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 font-mono text-xs">{record.name}</TableCell>
                  {/* Every value, not the first: a record with three
                      answers shown as one is a record you would
                      overwrite the other two of. */}
                  <TableCell className="max-w-xs px-4 py-2.5 font-mono text-xs break-all text-muted-foreground">
                    {record.values.join(", ")}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                    {record.ttl || "—"}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 whitespace-nowrap">
                    <div className="flex items-center justify-end gap-2">
                      <Button
                        variant="ghost"
                        size="xs"
                        aria-label={`Edit ${record.name}`}
                        className="size-6 p-0 text-muted-foreground"
                        onClick={() => setEditing({ record })}
                      >
                        <PencilIcon className="size-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="xs"
                        aria-label={`Delete ${record.name}`}
                        className="size-6 p-0 text-muted-foreground hover:text-destructive"
                        onClick={() => setDeleting(record)}
                      >
                        <Trash2Icon className="size-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}

      {records?.length === 0 && !missing && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            This zone has no records the provider will report.
          </CardContent>
        </Card>
      )}

      {editing && zone && (
        <RecordDialog
          base={base}
          zone={zone}
          record={editing.record}
          cloudflare={credential?.provider === "cloudflare"}
          onClose={() => setEditing(null)}
          onWritten={() => {
            load();
            setEditing(null);
          }}
        />
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(v) => !v && setDeleting(null)}
        title={`Delete the ${deleting?.type} record for ${deleting?.name}?`}
        description="Everything at that name and type goes, however many values it holds. Whatever was resolving through it stops."
        confirmWord={deleting?.name}
        onConfirm={async () => {
          if (!deleting || !zone) return;
          await api.del(
            `${base}/records?zone=${encodeURIComponent(zone.id)}` +
              `&name=${encodeURIComponent(deleting.name)}` +
              `&type=${encodeURIComponent(deleting.type)}`,
          );
          load();
          setDeleting(null);
        }}
      />
    </>
  );
}

// One dialog for both writing and editing, because the daemon has one
// operation: a write creates the record or replaces whatever is at that
// name and type. A separate "edit" would be the same call with the name
// filled in, which is what this is.
function RecordDialog({
  base,
  zone,
  record,
  cloudflare,
  onClose,
  onWritten,
}: {
  base: string;
  zone: DNSZone;
  record?: DNSRecord;
  cloudflare: boolean;
  onClose: () => void;
  onWritten: () => void;
}) {
  const [name, setName] = useState(record?.name ?? zone.name);
  const [type, setType] = useState(record?.type ?? "A");
  const [values, setValues] = useState((record?.values ?? []).join("\n"));
  const [ttl, setTTL] = useState(String(record?.ttl || 300));
  const [proxied, setProxied] = useState(record?.proxied ?? false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Proxying only means anything for the types Cloudflare can sit in
  // front of, and sending it for the rest is refused.
  const proxiable = cloudflare && (type === "A" || type === "AAAA" || type === "CNAME");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.put(`${base}/records?zone=${encodeURIComponent(zone.id)}`, {
        name,
        type,
        values: values.split("\n"),
        ttl: Number(ttl) || 300,
        proxied: proxiable ? proxied : false,
      });
      onWritten();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{record ? "Edit record" : "New record"}</DialogTitle>
            <DialogDescription>
              In <code>{zone.name}</code>. Writing replaces every value at this name and type — a
              record with three answers saved with one keeps one.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-5">
            <ErrorAlert error={error} className="mb-0" />

            {/* Name first and full width: it is what the record is,
                and the two short fields beside each other under it read
                as its settings rather than as three peers. */}
            <TextField
              label="Name"
              spellCheck={false}
              value={name}
              onChange={(e) => setName(e.target.value)}
              hint="The full name, not the part before the domain."
            />

            <div className="flex items-start gap-4">
              <SearchableSelect
                label="Type"
                fieldClassName="w-36"
                searchable={false}
                value={type}
                onChange={setType}
                choices={RECORD_TYPES.map((t) => ({ value: t, label: t }))}
              />
              <TextField
                label="TTL"
                fieldClassName="w-32"
                value={ttl}
                onChange={(e) => setTTL(e.target.value)}
                hint="Seconds"
              />
            </div>

            <TextAreaField
              label="Values"
              spellCheck={false}
              rows={3}
              value={values}
              onChange={(e) => setValues(e.target.value)}
              hint="One per line. A record is a list at both providers."
            />

            {proxiable && (
              <label className="flex items-start gap-2.5 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  checked={proxied}
                  onChange={(e) => setProxied(e.target.checked)}
                  className="mt-0.5"
                />
                <span>
                  Proxy through Cloudflare. The name resolves to Cloudflare rather than to this
                  value — which hides the host, and means Traefik never sees the original address.
                </span>
              </label>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!name || !values.trim()}>
              Save
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
