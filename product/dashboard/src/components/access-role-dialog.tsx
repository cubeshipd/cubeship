"use client";

import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { MultiSelect } from "@/components/multi-select";
import { SearchableSelect } from "@/components/searchable-select";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import {
  type AccessRole,
  api,
  type Datastore,
  type Grant,
  type Level,
  type ObjectStore,
  type ResourceInfo,
} from "@/lib/api";
import { message } from "@/lib/errors";

export const RESOURCE_LABEL: Record<string, string> = {
  projects: "Projects",
  apps: "Apps",
  domains: "Domains",
  databases: "Databases",
  storage: "Object storage",
  servers: "Servers",
  templates: "Templates",
  backups: "Backups",
  registry: "This instance's registry",
  registries: "Registries",
  git: "Git providers",
  dns: "DNS providers",
  credentials: "Credentials",
  certificates: "Certificates",
  firewall: "Firewall",
  settings: "Settings",
  audit: "Audit log",
};

const LEVELS: { value: Level; label: string }[] = [
  { value: "none", label: "None" },
  { value: "view", label: "View" },
  { value: "manage", label: "Manage" },
];

// What a role grants, in a sentence short enough for a table cell.
export function summarize(role: AccessRole): string {
  const granted = role.grants.filter((g) => g.level !== "none");
  if (granted.length === 0) return "Nothing";
  return granted
    .map((g) => {
      let part = `${RESOURCE_LABEL[g.resource] ?? g.resource}: ${g.level}`;
      if (g.secrets) part += " + secrets";
      if (g.items) part += ` (${g.items.join(", ") || "none"})`;
      return part;
    })
    .join(" · ");
}

type Row = { level: Level; secrets: boolean; items: string[] };

// Composing a role: every resource, one row each — how much of it, whether
// it reads secrets, and which ones.
//
// **Every resource is a row, granted or not.** A list of only what is
// granted says nothing about what is not, which is the half somebody
// shaping an agent's access is here to check.
export function AccessRoleDialog({
  open,
  role,
  resources,
  onOpenChange,
  onSaved,
}: {
  open: boolean;
  role: AccessRole | null;
  resources: ResourceInfo[];
  onOpenChange: (v: boolean) => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [rows, setRows] = useState<Record<string, Row>>({});
  const [items, setItems] = useState<Record<string, string[]>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setName(role?.name ?? "");
    setDescription(role?.description ?? "");
    setError(null);
    const next: Record<string, Row> = {};
    for (const info of resources) {
      const g = role?.grants.find((x) => x.resource === info.resource);
      next[info.resource] = {
        level: g?.level ?? "none",
        secrets: !!g?.secrets,
        items: g?.items ?? [],
      };
    }
    setRows(next);
  }, [open, role, resources]);

  // What an item can be, by what an item is.
  useEffect(() => {
    if (!open) return;
    Promise.all([
      api.get<{ slug: string }[]>("/projects").catch(() => []),
      api.get<Datastore[]>("/datastores").catch(() => []),
      api.get<ObjectStore[]>("/objectstores").catch(() => []),
    ]).then(([projects, databases, stores]) =>
      setItems({
        project: projects.map((p) => p.slug),
        database: databases.map((d) => d.name),
        "object store": stores.map((s) => s.name),
      }),
    );
  }, [open]);

  function set(resource: string, change: Partial<Row>) {
    setRows((r) => ({ ...r, [resource]: { ...r[resource], ...change } }));
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const grants: Grant[] = resources
      .filter((info) => rows[info.resource]?.level !== "none")
      .map((info) => {
        const row = rows[info.resource];
        return {
          resource: info.resource,
          level: row.level,
          secrets: info.secrets && row.secrets,
          // None picked is every one: a role that names no project is
          // not a role that reaches none.
          items: info.items && row.items.length > 0 ? row.items : null,
        };
      });
    try {
      const body = { name, description, grants };
      if (role) await api.put(`/roles/${role.id}`, body);
      else await api.post("/roles", body);
      onSaved();
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-3xl">
        <form onSubmit={submit} className="flex min-h-0 flex-1 flex-col">
          <DialogHeader className="shrink-0 border-border border-b p-4 pr-12">
            <DialogTitle>{role ? `Edit ${role.name}` : "New role"}</DialogTitle>
          </DialogHeader>

          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
            <ErrorAlert error={error} />
            <div className="grid gap-4 sm:grid-cols-2">
              <TextField
                label="Name"
                value={name}
                autoFocus
                onChange={(e) => setName(e.target.value)}
                placeholder="Deploy web"
              />
              <TextField
                label="Description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="What it is for"
              />
            </div>

            <div className="border border-border">
              <div className="grid grid-cols-[minmax(0,1fr)_7.5rem_4.5rem_minmax(0,1.4fr)] items-center gap-3 border-border border-b px-3 py-2 font-mono text-[10px] text-subtle-foreground uppercase tracking-wide">
                <span>Resource</span>
                <span>Access</span>
                <span>Secrets</span>
                <span>Which</span>
              </div>
              {resources.map((info) => {
                const row = rows[info.resource];
                if (!row) return null;
                const off = row.level === "none";
                return (
                  <div
                    key={info.resource}
                    className="grid grid-cols-[minmax(0,1fr)_7.5rem_4.5rem_minmax(0,1.4fr)] items-center gap-3 border-border border-b px-3 py-2 last:border-b-0"
                  >
                    <span className={off ? "text-muted-foreground text-sm" : "text-sm"}>
                      {RESOURCE_LABEL[info.resource] ?? info.resource}
                    </span>
                    <SearchableSelect
                      value={row.level}
                      onChange={(v) => set(info.resource, { level: v as Level })}
                      choices={LEVELS}
                      searchable={false}
                    />
                    <span className="flex h-10 items-center">
                      {info.secrets && (
                        <Switch
                          checked={row.secrets && !off}
                          disabled={off}
                          onCheckedChange={(on) => set(info.resource, { secrets: on })}
                          aria-label={`Read secrets on ${RESOURCE_LABEL[info.resource]}`}
                        />
                      )}
                    </span>
                    {info.items ? (
                      <MultiSelect
                        none={`Every ${info.items_are}`}
                        values={row.items}
                        onChange={(v) => set(info.resource, { items: v })}
                        choices={(items[info.items_are ?? ""] ?? []).map((i) => ({
                          value: i,
                          label: i,
                        }))}
                        disabled={off}
                      />
                    ) : (
                      <span className="text-muted-foreground text-xs">All of it</span>
                    )}
                  </div>
                );
              })}
            </div>
            <p className="text-[11px] text-muted-foreground leading-relaxed">
              Secrets are what somebody set: variables, credentials, files in a bucket, backup
              downloads. Users and roles stay an admin's. A key given this role never reaches more
              than its owner does.
            </p>
          </div>

          <DialogFooter className="mx-0 mb-0 shrink-0 rounded-none border-border border-t p-4">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!name.trim()}>
              {role ? "Save" : "Create role"}
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// What a role allows, listed: every resource it reaches, how far, and which
// ones. Opened from the roles table, which shows only names.
export function AccessRoleInfo({
  role,
  resources,
  onOpenChange,
}: {
  role: AccessRole | null;
  resources: ResourceInfo[];
  onOpenChange: (v: boolean) => void;
}) {
  const granted = resources
    .map((info) => ({ info, grant: role?.grants.find((g) => g.resource === info.resource) }))
    .filter((r) => r.grant && r.grant.level !== "none");

  return (
    <Dialog open={role !== null} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-xl">
        <DialogHeader className="shrink-0 border-border border-b p-4 pr-12">
          <DialogTitle>{role?.name}</DialogTitle>
          {role?.description && <p className="text-muted-foreground text-xs">{role.description}</p>}
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          {role?.system === "admin" ? (
            <p className="text-sm">
              Everything on this instance: every resource at manage with its secrets, users and
              roles, and building source on this host.
            </p>
          ) : granted.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              Nothing. Somebody holding it can sign in and see their own account.
            </p>
          ) : (
            <ul className="divide-y divide-border border border-border">
              {granted.map(({ info, grant }) => (
                <li
                  key={info.resource}
                  className="flex items-start justify-between gap-4 px-3 py-2.5"
                >
                  <span className="text-sm">{RESOURCE_LABEL[info.resource] ?? info.resource}</span>
                  <span className="flex min-w-0 flex-col items-end text-right">
                    <span className="text-xs">
                      {grant?.level === "manage" ? "View and manage" : "View"}
                      {grant?.secrets ? " · reads secrets" : ""}
                    </span>
                    {info.items && (
                      <span className="truncate font-mono text-[11px] text-muted-foreground">
                        {grant?.items
                          ? grant.items.join(", ") || "none"
                          : `every ${info.items_are}`}
                      </span>
                    )}
                  </span>
                </li>
              ))}
            </ul>
          )}
          {role?.system !== "admin" && (
            <p className="mt-3 text-[11px] text-muted-foreground">
              Anything not listed is not reached. Users and roles stay an admin's.
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
