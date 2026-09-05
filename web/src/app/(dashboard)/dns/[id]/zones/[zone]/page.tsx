"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronLeftIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { use, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { PageHeader } from "@/components/page-header";
import { SearchBar } from "@/components/search-bar";
import { SearchableSelect } from "@/components/searchable-select";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
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
export default function ZoneRecords({ params }: PageProps<"/dns/[id]/zones/[zone]">) {
  const { id, zone: zoneName } = use(params);
  const name = decodeURIComponent(zoneName);
  const queries = useQueryClient();

  const [editing, setEditing] = useState<{ record?: DNSRecord } | null>(null);
  const [deleting, setDeleting] = useState<DNSRecord | null>(null);
  const [query, setQuery] = useState("");

  const base = `/dns/${id}`;

  const credentials = useQuery({
    queryKey: ["dns"],
    queryFn: () => api.get<DNSCredential[]>(`/dns`),
  });
  const credential = credentials.data?.find((c) => String(c.id) === id) ?? null;

  // The name in the path is resolved against what the credential can
  // actually reach, so a link to a zone this credential lost access to
  // says so rather than failing on the first record call.
  //
  // The listing is the same query the zones page ran, under the same
  // key, so arriving here from there costs nothing.
  const zones = useQuery({
    queryKey: ["dns", id, "zones"],
    queryFn: () => api.get<DNSZone[]>(`${base}/zones`),
    enabled: Boolean(base),
  });
  const zone = zones.data?.find((z) => z.name === name) ?? null;
  const missing = zones.isSuccess && zone === null;

  const records = useQuery({
    queryKey: ["dns", id, "records", zone?.id],
    queryFn: () =>
      api.get<DNSRecord[]>(`${base}/records?zone=${encodeURIComponent(zone?.id ?? "")}`),
    enabled: Boolean(base && zone),
  });

  // One invalidation rather than a refetch call: anything else showing
  // this zone's records is looking at the same key and is wrong too.
  const reread = () => queries.invalidateQueries({ queryKey: ["dns", id, "records", zone?.id] });

  const error = credentials.error ?? zones.error ?? records.error;

  const q = query.trim().toLowerCase();
  const all = records.data ?? null;
  const filtered =
    all === null
      ? null
      : all.filter(
          (r) =>
            r.name.toLowerCase().includes(q) ||
            r.type.toLowerCase().includes(q) ||
            r.values.some((v) => v.toLowerCase().includes(q)),
        );

  // The widths add up to 100 and are proportions, not pixels. Value
  // takes the slack because it is the one that varies — and it is
  // capped rather than allowed to size to its content, which is what
  // used to drag the whole table off screen when a DKIM key landed in
  // it. The full value is on the row's own title.
  const columns: Column<DNSRecord>[] = [
    {
      id: "type",
      header: "Type",
      width: 10,
      sortBy: (r) => r.type,
      cell: (r) => <span className="font-mono text-xs text-muted-foreground">{r.type}</span>,
    },
    {
      id: "name",
      header: "Name",
      width: 28,
      sortBy: (r) => r.name,
      cell: (r) => (
        <span className="font-mono text-xs" title={r.name}>
          {r.name}
        </span>
      ),
    },
    {
      id: "value",
      header: "Value",
      width: 42,
      cell: (r) => (
        <span className="font-mono text-xs text-muted-foreground" title={r.values.join("\n")}>
          {r.values.join(", ")}
        </span>
      ),
    },
    {
      id: "ttl",
      header: "TTL",
      width: 8,
      sortBy: (r) => r.ttl,
      cell: (r) => (
        <span className="font-mono text-xs text-muted-foreground">{r.ttl || "\u2014"}</span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 12,
      align: "right",
      cell: (r) => (
        <div className="flex items-center justify-end gap-2">
          <Button
            variant="ghost"
            size="xs"
            aria-label={`Edit ${r.name}`}
            className="size-6 p-0 text-muted-foreground"
            onClick={() => setEditing({ record: r })}
          >
            <PencilIcon className="size-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="xs"
            aria-label={`Delete ${r.name}`}
            className="size-6 p-0 text-muted-foreground hover:text-destructive"
            onClick={() => setDeleting(r)}
          >
            <Trash2Icon className="size-3.5" />
          </Button>
        </div>
      ),
    },
  ];

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
          all &&
          all.length > 0 && (
            <SearchBar
              value={query}
              onChange={setQuery}
              placeholder="Filter by name, type or value"
              trailing={
                <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
                  {filtered?.length ?? 0}/{all.length}
                </span>
              }
            />
          )
        }
      />

      <ErrorAlert error={error ? message(error) : null} />

      {missing && (
        <Notice tone="warning">
          This credential does not reach <code>{name}</code>. Either the zone is gone, or the token
          was scoped to a different one.
        </Notice>
      )}

      {!missing && (
        <DataTable
          columns={columns}
          rows={filtered}
          rowKey={(r) => `${r.name}-${r.type}`}
          empty={
            query
              ? `Nothing matches \u201c${query}\u201d.`
              : "This zone has no records the provider will report."
          }
        />
      )}

      {editing && zone && (
        <RecordDialog
          base={base}
          zone={zone}
          record={editing.record}
          cloudflare={credential?.provider === "cloudflare"}
          onClose={() => setEditing(null)}
          onWritten={() => {
            reread();
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
          reread();
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
  // One input per value, not a textarea: a value is a single line —
  // an address, a host, a key — and a box you can put line breaks in
  // invites putting one in the middle of a DKIM key. A record is still
  // a list, so the list is a list of fields.
  const [values, setValues] = useState<string[]>(record?.values.length ? record.values : [""]);
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
        values,
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

            {/* Twelve columns, so the two short fields can be short
                without the two long ones having to match them. What a
                record is reads across the first row and what it points
                at across the second. */}
            <div className="grid grid-cols-12 items-start gap-4">
              <SearchableSelect
                label="Type"
                fieldClassName="col-span-3"
                searchable={false}
                value={type}
                onChange={setType}
                choices={RECORD_TYPES.map((t) => ({ value: t, label: t }))}
              />
              <TextField
                label="Name"
                fieldClassName="col-span-9"
                spellCheck={false}
                value={name}
                onChange={(e) => setName(e.target.value)}
                hint="The full name, not the part before the domain."
              />

              <div className="col-span-9 space-y-2">
                <Label className="text-xs text-muted-foreground">Value</Label>
                {values.map((value, i) => (
                  // The index is the identity: these are positions in a
                  // list someone is editing, and two identical values
                  // are two rows that must stay two rows.
                  // biome-ignore lint/suspicious/noArrayIndexKey: positions, not data
                  <div key={i} className="flex items-center gap-2">
                    <Input
                      spellCheck={false}
                      className="h-10 px-3 text-sm"
                      value={value}
                      onChange={(e) =>
                        setValues(values.map((v, j) => (j === i ? e.target.value : v)))
                      }
                    />
                    {values.length > 1 && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="xs"
                        aria-label="Remove this value"
                        className="size-8 shrink-0 p-0 text-muted-foreground hover:text-destructive"
                        onClick={() => setValues(values.filter((_, j) => j !== i))}
                      >
                        <Trash2Icon className="size-3.5" />
                      </Button>
                    )}
                  </div>
                ))}
                <button
                  type="button"
                  onClick={() => setValues([...values, ""])}
                  className="text-xs text-muted-foreground hover:text-foreground"
                >
                  + Add another value
                </button>
              </div>

              <TextField
                label="TTL"
                fieldClassName="col-span-3"
                value={ttl}
                onChange={(e) => setTTL(e.target.value)}
                hint="Seconds"
              />
            </div>

            {/* A switch, not a checkbox: a checkbox picks a thing out
                of a list — which is what the ones in the tables do —
                and this turns a behaviour on. */}
            {proxiable && (
              <div className="flex items-start gap-3">
                <Switch
                  id="proxied"
                  checked={proxied}
                  onCheckedChange={setProxied}
                  className="mt-0.5"
                />
                {/* A plain label element, not the Label primitive: that
                    one is uppercased with wide tracking as the house
                    style for a field's *name*, and a sentence shouted
                    in caps is unreadable. */}
                <label htmlFor="proxied" className="text-xs leading-relaxed text-muted-foreground">
                  Proxy through Cloudflare. The name resolves to Cloudflare rather than to this
                  value — which hides the host, and means Traefik never sees the original address.
                </label>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <ActionButton
              type="submit"
              busy={busy}
              disabled={!name || values.every((v) => !v.trim())}
            >
              Save
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
