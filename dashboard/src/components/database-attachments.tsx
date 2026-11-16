"use client";
import { PlusIcon, Trash2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { type App, api, type Datastore, type DatastoreAttachment, datastorePath } from "@/lib/api";
import { message } from "@/lib/errors";

// The wiring, as a table like every other list here.
//
// The whole row opens the app rather than the name being a link inside
// it: two click targets for one destination is one of them that people
// miss. The detach button stops the click from reaching the row, or
// removing a wiring would also navigate away from the page that shows
// it worked.
function attachmentColumns(onDetach: (app: string) => void): Column<DatastoreAttachment>[] {
  return [
    {
      id: "app",
      header: "App",
      width: 90,
      sortBy: (a) => a.app,
      cell: (a) => (
        <span className="flex items-center gap-2">
          <span className="truncate font-mono text-sm">{a.app}</span>
          {/* The prefix, and only when there is one. It is what
              changes the names an app reads, and this is the only
              place it would ever be visible. */}
          {a.prefix && (
            <code className="shrink-0 border border-border bg-secondary/50 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
              {a.prefix}
            </code>
          )}
        </span>
      ),
    },
    {
      id: "detach",
      header: "",
      width: 10,
      align: "right",
      cell: (a) => (
        <RowActions>
          <RowAction
            icon={Trash2Icon}
            label={`Detach ${a.app}`}
            danger
            onClick={() => onDetach(a.app)}
          />
        </RowActions>
      ),
    },
  ];
}

// Which apps get this database's connection variables.
//
// The variable names are listed rather than summarised, because the
// question somebody has on this screen is "what do I read in my code",
// and the answer is exactly this list.
export function Attachments({
  datastore,
  onChanged,
}: {
  datastore: Datastore;
  onChanged: () => void;
}) {
  const router = useRouter();
  const [attaching, setAttaching] = useState(false);
  const [detaching, setDetaching] = useState<string | null>(null);
  const [apps, setApps] = useState<App[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Loaded here rather than when the dialog opens, and not only to save
  // a wait. A select with nothing in it yet is a disabled select, and a
  // dialog whose first field is disabled opens focused on whatever
  // comes after it.
  useEffect(() => {
    api
      .get<App[]>("/apps")
      .then(setApps)
      .catch((e) => setError(message(e)));
  }, []);

  async function detach(appRef: string) {
    setError(null);
    try {
      await api.del(`${datastorePath(datastore.name)}/attachments/${appRef}`);
      onChanged();
    } catch (err) {
      setError(message(err));
    }
  }

  return (
    <>
      <SectionHeader
        title="Attached apps"
        sub={`Attached apps receive ${datastore.var_stem}_URL and its connection details on their next deploy.`}
        actions={
          <Button onClick={() => setAttaching(true)}>
            <PlusIcon />
            Attach an app
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={attachmentColumns((app) => setDetaching(app))}
        rows={datastore.attachments}
        search={{
          placeholder: "Filter apps",
          countLabel: { singular: "app", plural: "apps" },
          by: (a) => [a.app, a.prefix],
        }}
        rowKey={(a) => a.app}
        onRowClick={(a) => router.push(`/projects/${a.app}`)}
        empty="Nothing is attached yet."
      />

      <AttachDialog
        datastore={datastore}
        apps={apps}
        open={attaching}
        onOpenChange={setAttaching}
        onAttached={onChanged}
      />

      {/* A confirmation with no word to type. Detaching is undone by
          attaching again, so the guard only has to stop the misclick —
          typing a name is for what cannot be put back. */}
      <ConfirmDialog
        open={detaching !== null}
        onOpenChange={(open) => !open && setDetaching(null)}
        title="Detach this app?"
        description={
          <>
            <code className="text-foreground">{detaching}</code> keeps the variables it is running
            with until it is deployed again, and comes up without them after that. Attaching it
            again puts them back.
          </>
        }
        confirmLabel="Detach"
        onConfirm={async () => {
          if (detaching) await detach(detaching);
          setDetaching(null);
        }}
      />
    </>
  );
}

function AttachDialog({
  datastore,
  apps,
  open,
  onOpenChange,
  onAttached,
}: {
  datastore: Datastore;
  // Already loaded by the time this opens — see Attachments.
  apps: App[] | null;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onAttached: () => void;
}) {
  const [project, setProject] = useState("");
  const [environment, setEnvironment] = useState("");
  const [appName, setAppName] = useState("");
  const [prefix, setPrefix] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setProject("");
    setEnvironment("");
    setAppName("");
    setPrefix("");
    setError(null);
  }, [open]);

  // Everything that could still be attached. Apps already wired to this
  // database are left out: the daemon refuses them, and offering a
  // choice it would refuse is a form that argues with you.
  const available = useMemo(() => {
    const attached = new Set(datastore.attachments.map((a) => a.app));
    return (apps ?? []).filter((a) => !attached.has(a.reference));
  }, [apps, datastore.attachments]);

  // The three lists are derived from the apps rather than fetched
  // separately, which is one request instead of three and — more to the
  // point — means every project and environment offered has something
  // in it. Picking one and finding it empty is a dead end nobody should
  // be able to walk into.
  const projects = useMemo(() => [...new Set(available.map((a) => a.project))].sort(), [available]);
  const environments = useMemo(
    () =>
      [...new Set(available.filter((a) => a.project === project).map((a) => a.environment))].sort(),
    [available, project],
  );
  const candidates = useMemo(
    () => available.filter((a) => a.project === project && a.environment === environment),
    [available, project, environment],
  );

  // Snap to the only answer. Most instances are one project with one
  // environment, and there the app is the only real choice — three
  // fields where two of them can only be answered one way is two clicks
  // charged for nothing.
  useEffect(() => {
    if (!project && projects.length === 1) setProject(projects[0]);
  }, [project, projects]);
  useEffect(() => {
    if (!environment && environments.length === 1) setEnvironment(environments[0]);
  }, [environment, environments]);

  const reference = project && environment && appName && `${project}/${environment}/${appName}`;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(`${datastorePath(datastore.name)}/attachments`, { app: reference, prefix });
      onAttached();
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>Attach an app</DialogTitle>
            <DialogDescription>
              It will receive{" "}
              <code className="text-foreground">
                {prefix}
                {datastore.var_stem}_URL
              </code>{" "}
              and its parts on its next deploy.
            </DialogDescription>
          </DialogHeader>

          <div className="-mx-2 max-h-[55vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} />

            {apps && available.length === 0 ? (
              <Notice>
                Every app on this instance is already attached, or there are none yet.
              </Notice>
            ) : (
              <>
                {/* Narrowed one level at a time, because an app's name
                    only means anything inside an environment inside a
                    project — and a flat list of every app on the
                    instance is a list nobody can find anything in once
                    there are more than a handful. */}
                <div className="grid gap-4 sm:grid-cols-2">
                  <SearchableSelect
                    label="Project"
                    placeholder="Choose one"
                    empty="No project has an app to attach."
                    busy={!apps}
                    choices={projects.map((p) => ({ value: p, label: p }))}
                    value={project}
                    onChange={(next) => {
                      setProject(next);
                      setEnvironment("");
                      setAppName("");
                    }}
                  />
                  <SearchableSelect
                    label="Environment"
                    placeholder="Choose one"
                    empty="This project has no app to attach."
                    disabled={!project}
                    choices={environments.map((e) => ({ value: e, label: e }))}
                    value={environment}
                    onChange={(next) => {
                      setEnvironment(next);
                      setAppName("");
                    }}
                  />
                </div>

                <SearchableSelect
                  label="App"
                  placeholder="Choose an app"
                  empty="Every app here is already attached."
                  disabled={!environment}
                  choices={candidates.map((a) => ({
                    value: a.name,
                    label: a.name,
                  }))}
                  value={appName}
                  onChange={setAppName}
                />
              </>
            )}

            <TextField
              label="Prefix"
              value={prefix}
              spellCheck={false}
              placeholder="ANALYTICS_"
              onChange={(e) => setPrefix(e.target.value.toUpperCase())}
              hint={`Leave empty unless the app already gets ${datastore.var_stem}_URL from another database — two of those would be one variable with two values. Anything writing different names, like a cache beside a database, needs no prefix.`}
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!reference}>
              Attach
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
