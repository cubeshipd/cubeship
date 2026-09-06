"use client";

import {
  ChevronLeftIcon,
  EyeIcon,
  EyeOffIcon,
  PlugIcon,
  SettingsIcon,
  Trash2Icon,
} from "lucide-react";
import Link from "next/link";
import { use, useCallback, useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { CopyField } from "@/components/copy-field";
import { ErrorAlert } from "@/components/error-alert";
import { MetricsSection } from "@/components/metrics-section";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
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
  ApiError,
  type App,
  api,
  type Datastore,
  type DatastoreCredentials,
  datastoreLabel,
  datastorePath,
} from "@/lib/api";
import { message } from "@/lib/errors";

// One database, on one page.
//
// Sections rather than tabs. There are three things to know about a
// database — how hard it is working, how to connect to it, and what is
// connected to it — and none of them is an alternative to the others:
// hiding two of three behind a click made you click through all of them
// every time you opened it.
//
// Monitoring is first because it is the question you have before you
// know you have one.
export default function DatastorePage({ params }: PageProps<"/databases/[name]">) {
  const { name } = use(params);
  return <Detail name={name} />;
}

function Detail({ name }: { name: string }) {
  const [datastore, setDatastore] = useState<Datastore | null>(null);
  const [error, setError] = useState<string | null>(null);

  const path = datastorePath(name);
  const reload = useCallback(() => {
    api
      .get<Datastore>(path)
      .then(setDatastore)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // Provisioning is a container being pulled and started, detached from
  // the request that asked for it — so this is the one screen that has
  // to go back and look. It stops the moment it settles.
  useEffect(() => {
    if (datastore?.status !== "provisioning") return;
    const timer = setInterval(reload, 2000);
    return () => clearInterval(timer);
  }, [datastore?.status, reload]);

  if (error && !datastore) return <ErrorAlert error={error} />;
  if (!datastore) return null;

  return (
    <>
      <Link
        href="/databases"
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        databases
      </Link>

      <PageHeader
        title={
          <span className="font-mono text-lg tracking-normal normal-case">{datastore.name}</span>
        }
        sub={
          <span className="flex items-center gap-2">
            {datastoreLabel(datastore.engine)} {datastore.version}
            <StatusBadge value={datastore.status} />
          </span>
        }
        actions={
          <Button
            variant="outline"
            nativeButton={false}
            render={
              <Link href={`/databases/${datastore.name}/settings`}>
                <SettingsIcon />
                Settings
              </Link>
            }
          />
        }
      />

      <ErrorAlert error={error} />

      {/* The reason a database did not come up is the tail of what the
          engine printed, and it appears nowhere else — the container it
          came from has been removed. */}
      {datastore.status === "failed" && datastore.error && (
        <Card className="mb-4 border-l-2 border-destructive/30 border-l-destructive">
          <CardContent>
            <div className="text-[11px] tracking-[0.12em] text-destructive uppercase">
              It did not start
            </div>
            <pre className="mt-2 overflow-x-auto font-mono text-xs whitespace-pre-wrap text-muted-foreground">
              {datastore.error}
            </pre>
          </CardContent>
        </Card>
      )}

      <MetricsSection path={path} />
      <Connection datastore={datastore} />
      <Attachments datastore={datastore} onChanged={reload} />
    </>
  );
}

// Everything needed to connect, as fields you can copy and cannot edit.
//
// Read-only inputs rather than a table of values: a connection string
// is long, and a field scrolls inside itself and takes one click to
// select, where a wrapped line of prose gives you three lines and a
// chance to miss one. None of it is editable — see Service.Update on
// the daemon for why almost none of it can be.
function Connection({ datastore }: { datastore: Datastore }) {
  const [creds, setCreds] = useState<DatastoreCredentials | null>(null);
  const [forbidden, setForbidden] = useState(false);
  const [revealed, setRevealed] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setCreds(null);
    setForbidden(false);
    api
      .get<DatastoreCredentials>(`${datastorePath(datastore.name)}/credentials`)
      .then(setCreds)
      .catch((e) => {
        // Reading the login is an admin's. A member still gets the
        // address and the user name, which is most of what is on this
        // screen — so the refusal is a note, not an error.
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(message(e));
      });
  }, [datastore.name]);

  return (
    <>
      <SectionHeader
        title="Connection"
        sub="Apps on this instance reach it by container name on the shared network. Nothing else can, unless it is published."
        actions={
          creds && (
            <Button variant="ghost" size="xs" onClick={() => setRevealed(!revealed)}>
              {revealed ? <EyeOffIcon /> : <EyeIcon />}
              {revealed ? "Hide" : "Reveal"}
            </Button>
          )
        }
      />

      <ErrorAlert error={error} />

      {forbidden && (
        <Notice>
          The password and the connection strings need the admin role. Everything else about this
          database is below.
        </Notice>
      )}

      <Card className="mb-4">
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <CopyField label="Username" value={datastore.username} />
          {datastore.database && <CopyField label="Database" value={datastore.database} />}

          {creds && (
            <CopyField
              label="Password"
              value={creds.password}
              masked={!revealed}
              hint="Copyable while hidden — nothing has to read it to use it."
            />
          )}

          <CopyField
            label="Internal host"
            value={`${datastore.host}:${datastore.port}`}
            hint="The container's own name on the shared network."
          />

          {creds && (
            <CopyField
              label="Internal URI"
              value={creds.internal_uri}
              fieldClassName="sm:col-span-2"
              hint="What an attached app already receives as DATABASE_URL. This is for anything wired by hand."
            />
          )}

          {creds?.external_uri && (
            <CopyField
              label="External URI"
              value={creds.external_uri}
              fieldClassName="sm:col-span-2"
              hint="From off this host, on the port this database is published on. There is no TLS in front of it."
            />
          )}
        </CardContent>
      </Card>
    </>
  );
}

// Which apps get this database's connection variables.
//
// The variable names are listed rather than summarised, because the
// question somebody has on this screen is "what do I read in my code",
// and the answer is exactly this list.
function Attachments({ datastore, onChanged }: { datastore: Datastore; onChanged: () => void }) {
  const [attaching, setAttaching] = useState(false);
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
        sub="Each one receives the connection string as environment variables, from its next deploy onwards. They may be in any project."
        actions={
          <Button variant="outline" size="sm" onClick={() => setAttaching(true)}>
            <PlugIcon />
            Attach an app
          </Button>
        }
      />

      <ErrorAlert error={error} />

      {datastore.attachments.length === 0 ? (
        <Notice>
          Nothing is attached, so no app on this instance can reach this database yet. Attaching one
          gives it <code className="text-foreground">DATABASE_URL</code> and its parts.
        </Notice>
      ) : (
        <Card className="mb-4">
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>App</TableHead>
                  <TableHead>Variables it receives</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {datastore.attachments.map((a) => (
                  <TableRow key={a.app}>
                    <TableCell className="font-mono">
                      <Link href={`/projects/${a.app}`} className="hover:text-primary">
                        {a.app}
                      </Link>
                    </TableCell>
                    {/* The variable names wrap rather than scroll. The
                        question this column answers is "what do I read
                        in my code", and an answer you have to drag
                        sideways to finish reading is a worse one. */}
                    <TableCell className="whitespace-normal">
                      <span className="flex flex-wrap gap-1">
                        {a.variables.map((v) => (
                          <code
                            key={v}
                            className="border border-border bg-secondary/50 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground"
                          >
                            {v}
                          </code>
                        ))}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`Detach ${a.app}`}
                        onClick={() => detach(a.app)}
                      >
                        <Trash2Icon className="size-3.5" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Notice>
        A container keeps the environment it was created with, so attaching or detaching changes
        nothing until the app is deployed again.
      </Notice>

      <AttachDialog
        datastore={datastore}
        apps={apps}
        open={attaching}
        onOpenChange={setAttaching}
        onAttached={onChanged}
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
              It will receive <code className="text-foreground">{prefix}DATABASE_URL</code> and its
              parts on its next deploy.
            </DialogDescription>
          </DialogHeader>

          <div className="-mx-2 max-h-[55vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} className="mb-0" />

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
                    hint: a.description || undefined,
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
              hint="Leave empty unless this app already has a database. Two under the same prefix would be one variable with two values."
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
