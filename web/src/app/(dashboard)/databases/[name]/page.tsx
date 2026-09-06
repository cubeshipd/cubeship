"use client";

import {
  ChevronLeftIcon,
  EyeIcon,
  EyeOffIcon,
  PlayIcon,
  PlugIcon,
  PowerIcon,
  SettingsIcon,
  Trash2Icon,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogs } from "@/components/container-logs";
import { CopyField } from "@/components/copy-field";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { MetricsSection } from "@/components/metrics-section";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchableSelect } from "@/components/searchable-select";
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
  ApiError,
  type App,
  api,
  DATASTORE_STOPPED,
  type Datastore,
  type DatastoreAttachment,
  type DatastoreCredentials,
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

  return (
    <>
      <Link
        href="/databases"
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        databases
      </Link>

      {/* The header does not wait. Its name is in the URL — we are on
          this page because somebody asked for this database by name —
          so rendering it needs no round trip, and the page has a title
          the instant it is navigated to.
          
          It used to return null until the fetch landed, which meant
          every navigation blanked the content area and then filled it.
          A page that disappears before it appears reads as slow however
          fast the request was. */}
      <PageHeader
        title={<span className="font-mono text-lg tracking-normal normal-case">{name}</span>}
        actions={
          datastore && (
            <>
              <PowerButton datastore={datastore} onChanged={reload} />
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
            </>
          )
        }
      />

      <ErrorAlert error={error} />

      {!datastore && <LoadingList rows={5} />}

      {/* The reason a database did not come up is the tail of what the
          engine printed, and it appears nowhere else — the container it
          came from has been removed. */}
      {datastore?.status === "failed" && datastore.error && (
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

      {datastore && (
        <>
          <MetricsSection path={path} />
          <Connection datastore={datastore} />
          {/* Only once there is a container to have written one. Before
          that the endpoint answers 409, and an error box saying so is
          worse than the section not being there. */}
          {datastore.has_container && (
            <ContainerLogs
              path={path}
              sub="What the engine itself has printed. The first place to look when it refuses connections or will not start."
            />
          )}
          <Attachments datastore={datastore} onChanged={reload} />
        </>
      )}
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
            <Button
              variant="ghost"
              size="xs"
              onClick={() => setRevealed(!revealed)}
              aria-label={
                revealed ? "Hide the password and the URIs" : "Reveal the password and the URIs"
              }
            >
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
              // The password is inside it. Hiding one and printing the
              // other on the same card would be a lock on a door with
              // the key taped beside it — and this is the field
              // somebody leaves on screen while they go and paste it.
              masked={!revealed}
              fieldClassName="sm:col-span-2"
              hint={`What an attached app already receives as ${datastore.var_stem}_URL. This is for anything wired by hand.`}
            />
          )}

          {creds?.external_uri && (
            <CopyField
              label="External URI"
              value={creds.external_uri}
              masked={!revealed}
              fieldClassName="sm:col-span-2"
              hint="From off this host, on the port this database is published on. There is no TLS in front of it."
            />
          )}
        </CardContent>
      </Card>
    </>
  );
}

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
function Attachments({ datastore, onChanged }: { datastore: Datastore; onChanged: () => void }) {
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
        sub={`Each one receives ${datastore.var_stem}_URL and its parts, from its next deploy onwards. They may be in any project.`}
        actions={
          <Button variant="outline" size="sm" onClick={() => setAttaching(true)}>
            <PlugIcon />
            Attach an app
          </Button>
        }
      />

      <ErrorAlert error={error} />

      <DataTable
        columns={attachmentColumns((app) => setDetaching(app))}
        rows={datastore.attachments}
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

// Turning a database off and on.
//
// In the header rather than in settings, because it is not a setting:
// it is a thing you do, in the moment you decide to, and its result is
// the status badge two inches away. Deleting stays in settings, where
// what cannot be undone belongs.
function PowerButton({ datastore, onChanged }: { datastore: Datastore; onChanged: () => void }) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const off = datastore.status === DATASTORE_STOPPED || datastore.status === "failed";
  const path = datastorePath(datastore.name);

  async function run(action: "start" | "stop") {
    setBusy(true);
    setError(null);
    try {
      await api.post(`${path}/${action}`);
      onChanged();
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
    setConfirming(false);
  }

  return (
    <>
      <ErrorAlert error={error} />
      <ActionButton
        variant="outline"
        busy={busy}
        // Nothing to turn off while it is still coming up, and nothing
        // to turn on until it has a container to have been off.
        disabled={datastore.status === "provisioning"}
        onClick={() => (off ? run("start") : setConfirming(true))}
      >
        {off ? <PlayIcon /> : <PowerIcon />}
        {off ? "Start" : "Stop"}
      </ActionButton>

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Stop ${datastore.name}?`}
        description={
          <>
            The container stops and its data stays where it is — starting it again brings it back
            unchanged.{" "}
            {datastore.attachments.length > 0 ? (
              <>
                <strong>{datastore.attachments.map((a) => a.app).join(", ")}</strong> will keep
                running and start failing to connect.
              </>
            ) : (
              "No app is attached to it."
            )}
          </>
        }
        confirmLabel="Stop it"
        onConfirm={() => run("stop")}
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
