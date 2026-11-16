"use client";
import { BracesIcon, EyeIcon, EyeOffIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CopyButton } from "@/components/copy-button";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { FilterPanel } from "@/components/filter-panel";
import { GLIDE_FIELD_TRIGGER_CLASS, GlideSelect } from "@/components/glide-select";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchBar } from "@/components/search-bar";
import { SectionHeader } from "@/components/section-header";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, type EnvView, type ResolvedVar } from "@/lib/api";
import { message } from "@/lib/errors";

// One variable's value: hidden until asked for, copyable either way.
//
// Hidden by default because this table is most of a page of secrets —
// an app's environment is where its database password, its API keys and
// its signing secrets all end up — and it is the section somebody
// scrolls past with a screen share running. The copy button works while
// it is hidden, which is what makes hiding it cost nothing.
function SecretValue({ value }: { value: string }) {
  const [shown, setShown] = useState(false);
  return (
    <span className="flex items-center gap-1">
      <span className="min-w-0 flex-1 truncate font-mono text-xs">
        {shown ? value : "•".repeat(Math.min(value.length, 24)) || "—"}
      </span>
      <Button
        variant="ghost"
        size="xs"
        aria-label={shown ? "Hide this value" : "Reveal this value"}
        className="shrink-0 text-muted-foreground"
        onClick={() => setShown(!shown)}
      >
        {shown ? <EyeOffIcon className="size-3.5" /> : <EyeIcon className="size-3.5" />}
      </Button>
      <CopyButton value={value} label="Copy this value" className="shrink-0" />
    </span>
  );
}

// What the app's container runs with, and where each value came from.
//
// The filter keeps a long list searchable; the table stays in the page
// flow so every variable can be scanned with the page's normal scroll.
export function EnvVars({ reference }: { reference: string }) {
  const [view, setView] = useState<EnvView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [sourceFilter, setSourceFilter] = useState("");
  const [editing, setEditing] = useState<ResolvedVar | null>(null);
  const [adding, setAdding] = useState(false);
  const [unsetting, setUnsetting] = useState<ResolvedVar | null>(null);

  const path = `/apps/${reference}/env`;
  const reload = useCallback(() => {
    api
      .get<EnvView>(path)
      .then(setView)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  const all = view?.effective ?? null;
  const sourceOptions = useMemo(() => {
    const labels: Record<string, string> = {
      app: "App",
      datastore: "Database",
      environment: "Environment",
      objectstore: "Object storage",
      project: "Project",
    };
    const sources = [...new Set((all ?? []).map((variable) => variable.source))].sort((a, b) =>
      (labels[a] ?? a).localeCompare(labels[b] ?? b),
    );
    return [
      { value: "", label: "All sources" },
      ...sources.map((source) => ({ value: source, label: labels[source] ?? source })),
    ];
  }, [all]);
  const rows = useMemo(() => {
    if (all === null) return null;
    const needle = filter.trim().toLowerCase();
    return all.filter(
      (variable) =>
        (!needle || variable.key.toLowerCase().includes(needle)) &&
        (!sourceFilter || variable.source === sourceFilter),
    );
  }, [all, filter, sourceFilter]);

  const columns: Column<ResolvedVar>[] = [
    {
      id: "key",
      header: "Name",
      width: 32,
      sortBy: (v) => v.key,
      cell: (v) => <span className="font-mono text-xs break-all">{v.key}</span>,
    },
    {
      id: "value",
      header: "Value",
      width: 44,
      cell: (v) => <SecretValue value={v.value} />,
    },
    {
      id: "source",
      header: "Set at",
      width: 14,
      sortBy: (v) => v.source,
      cell: (v) => (
        <span className="text-xs text-muted-foreground">
          {sourceOptions.find((option) => option.value === v.source)?.label ?? v.source}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      // Only what this app set itself. A variable inherited from the
      // project, the environment or an attached database is not this
      // screen's to change — editing it here would look like it worked
      // and change nothing, and the level that owns it is where it is
      // owned.
      cell: (v) =>
        v.source === "app" ? (
          <RowActions>
            <RowAction icon={PencilIcon} label={`Edit ${v.key}`} onClick={() => setEditing(v)} />
            <RowAction
              icon={Trash2Icon}
              label={`Unset ${v.key}`}
              danger
              onClick={() => setUnsetting(v)}
            />
          </RowActions>
        ) : null,
    },
  ];

  return (
    <>
      <SectionHeader
        title="Environment"
        sub="The project's variables, then the environment's, then any database or bucket attached to this app, then the app's own. Setting one here overrides the levels above it."
        actions={
          <Button onClick={() => setAdding(true)}>
            <PlusIcon />
            Add variable
          </Button>
        }
      />
      <ErrorAlert error={error} />

      <FilterPanel>
        <GlideSelect
          label="Source"
          value={sourceFilter}
          startIcon={<BracesIcon className="size-4" />}
          options={sourceOptions}
          onChange={setSourceFilter}
          className="w-full"
          triggerClassName={GLIDE_FIELD_TRIGGER_CLASS}
        />
      </FilterPanel>
      <SearchBar
        value={filter}
        onChange={setFilter}
        placeholder="Filter by name"
        className="mb-4"
        count={
          all && rows
            ? { value: rows.length, singular: "variable", plural: "variables" }
            : undefined
        }
      />

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(v) => v.key}
        loadingRows={6}
        empty={filter.trim() || sourceFilter ? "Nothing matches these filters." : "No variables."}
      />

      <EnvVarDialog
        path={path}
        open={adding || editing !== null}
        variable={editing}
        onOpenChange={(open) => {
          if (open) return;
          setAdding(false);
          setEditing(null);
        }}
        onSaved={reload}
      />

      {/* A confirmation with no word to type: unsetting is undone by
          setting it again, so the guard only has to stop the misclick.
          It still needs one — the app comes up without the variable on
          its next deploy, which is a thing that happens later and
          elsewhere. */}
      <ConfirmDialog
        open={unsetting !== null}
        onOpenChange={(open) => !open && setUnsetting(null)}
        title="Unset this variable?"
        confirmLabel="Unset"
        description={
          <>
            <code className="text-foreground">{unsetting?.key}</code> goes from this app. Its
            container keeps the value it is running with until it is deployed again — and if a level
            above sets the same name, that one takes over.
          </>
        }
        onConfirm={async () => {
          if (!unsetting) return;
          await api.patch(path, { unset: [unsetting.key] });
          setUnsetting(null);
          reload();
        }}
      />
    </>
  );
}

// Adding one and editing one are the same dialog, because they are the
// same request: PATCH sets a name to a value whether or not it was
// already there.
function EnvVarDialog({
  path,
  open,
  variable,
  onOpenChange,
  onSaved,
}: {
  path: string;
  open: boolean;
  // The variable being edited, or null when adding.
  variable: ResolvedVar | null;
  onOpenChange: (v: boolean) => void;
  onSaved: () => void;
}) {
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setKey(variable?.key ?? "");
    setValue(variable?.value ?? "");
    setError(null);
  }, [open, variable]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.patch(path, { set: { [key.trim()]: value } });
      onSaved();
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
            <DialogTitle>{variable ? "Edit variable" : "Add variable"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <TextField
              label="Name"
              value={key}
              autoFocus={!variable}
              spellCheck={false}
              // The name is the identity of the row being edited.
              // Changing it here would set a second variable and leave
              // the first, which is not what "edit" means anywhere.
              disabled={variable !== null}
              onChange={(e) => setKey(e.target.value)}
            />
            <TextField
              label="Value"
              value={value}
              autoFocus={variable !== null}
              spellCheck={false}
              onChange={(e) => setValue(e.target.value)}
              hint="Takes effect on this app's next deploy — a container keeps the environment it was created with."
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!key.trim()}>
              Save
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
