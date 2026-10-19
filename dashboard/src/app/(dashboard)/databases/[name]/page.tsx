"use client";

import { useQueryClient } from "@tanstack/react-query";
import {
  AppWindowIcon,
  ArchiveIcon,
  BlocksIcon,
  CheckIcon,
  CpuIcon,
  EyeIcon,
  EyeOffIcon,
  LayoutDashboardIcon,
  NetworkIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { use, useCallback, useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogsDialog } from "@/components/container-logs";
import { CopyField } from "@/components/copy-field";
import { type Column, DataTable } from "@/components/data-table";
import {
  DatabaseAccess,
  DatabaseBackups,
  DatabaseLimits,
} from "@/components/database-settings-panels";
import { useDatabaseWorkspaceActions } from "@/components/database-workspace";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { MetricsSection } from "@/components/metrics-section";
import { Notice } from "@/components/notice";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchBar } from "@/components/search-bar";
import { SearchableSelect } from "@/components/searchable-select";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  ApiError,
  type App,
  api,
  can,
  DATASTORE_STOPPED,
  type Datastore,
  type DatastoreAttachment,
  type DatastoreCredentials,
  type DatastoreEngine,
  type DatastoreEngineExtension,
  datastorePath,
} from "@/lib/api";
import { message } from "@/lib/errors";

// One database, on one page, in tabs — the same shape as an app's.
//
// It was sections for a while, on the grounds that all three were short
// and none was an alternative to the others. Two of them are not short.
// A log is the whole of what the engine has printed, and the attached
// apps are a table with its own dialog and its own confirmations; below
// them the two things somebody opens this page for — is it working, and
// how do I connect to it — sat off the bottom of the screen exactly
// when the database was busy enough to be worth looking at.
//
// So the page opens on what is short and always wanted, and what is
// long, or is somewhere you go rather than something you read, is a tab
// beside it.
type Tab = "overview" | "extensions" | "apps" | "backups" | "access" | "resources";

const TAB_ALIASES: Record<string, Tab> = {
  overview: "overview",
  extensions: "extensions",
  apps: "apps",
  backups: "backups",
  access: "access",
  network: "access",
  resources: "resources",
  danger: "resources",
};

export default function DatastorePage({ params }: PageProps<"/databases/[name]">) {
  const { name } = use(params);
  return <Detail name={name} />;
}

function Detail({ name }: { name: string }) {
  // Which tab is open is view state rather than an identity, so it is
  // not a segment — but it is linkable, because the Backups screen's
  // whole job is "this one is not protected" and landing somebody on
  // Overview to hunt for the tab is the papercut that makes a report
  // not worth opening.
  //
  // **Through `useSearchParams` rather than `window`**, and the
  // difference is not style: this page renders on the server first,
  // where there is no window, so a `useState` initializer reading one
  // produces "overview" — and React keeps the state it hydrated with
  // rather than re-running the initializer, so the parameter was read
  // and silently thrown away. This hook is the one thing that has the
  // same answer on both sides.
  const asked = useSearchParams().get("tab");
  const [initial] = useState<Tab>(() => TAB_ALIASES[asked ?? ""] ?? "overview");
  const router = useRouter();
  const queryClient = useQueryClient();
  const me = useSession();
  const setWorkspaceActions = useDatabaseWorkspaceActions();
  const [datastore, setDatastore] = useState<Datastore | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Only the arrival decides it. After that the tab row does, and a
  // stale parameter must not drag somebody back when they click away.
  const [tab, setTab] = useState<Tab>(initial);
  const [stopOpen, setStopOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [logsOpen, setLogsOpen] = useState(false);
  const [powerBusy, setPowerBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const path = datastorePath(name);
  const reload = useCallback(() => {
    api
      .get<Datastore>(path)
      .then(setDatastore)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  const canManage = can(me, "databases", "manage");
  const isStopped = datastore?.status === DATASTORE_STOPPED || datastore?.status === "failed";
  const activeTab = datastore
    ? tab === "backups" && !datastore.can_back_up
      ? "access"
      : tab === "extensions" && datastore.engine !== "postgres"
        ? "overview"
        : tab
    : tab;
  const power = useCallback(
    async (action: "start" | "stop") => {
      if (!datastore) return;
      setPowerBusy(true);
      setActionError(null);
      try {
        await api.post(`${path}/${action}`);
        reload();
        await queryClient.invalidateQueries({ queryKey: ["datastores"] });
      } catch (cause) {
        setActionError(message(cause));
      } finally {
        setPowerBusy(false);
        setStopOpen(false);
      }
    },
    [datastore, path, queryClient, reload],
  );

  useEffect(() => {
    setWorkspaceActions({
      canStart: canManage && !!isStopped,
      canStop:
        canManage &&
        !!datastore?.has_container &&
        !isStopped &&
        datastore.status !== "provisioning",
      start: () => void power("start"),
      stop: () => setStopOpen(true),
      hasLogs: !!datastore?.has_container,
      logs: () => setLogsOpen(true),
      delete: canManage ? () => setDeleteOpen(true) : undefined,
      busy: powerBusy,
      error: actionError,
    });
    return () => setWorkspaceActions({});
  }, [actionError, canManage, datastore, isStopped, power, powerBusy, setWorkspaceActions]);

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
      {" "}
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
        <Tabs
          value={activeTab}
          onValueChange={(value) => setTab(value as Tab)}
          className="workspace-tabs"
        >
          <div className="app-tab-navigation">
            <TabsList variant="default" className="app-tab-list">
              <TabsTrigger value="overview">
                <LayoutDashboardIcon />
                Overview
              </TabsTrigger>
              {datastore.engine === "postgres" && (
                <TabsTrigger value="extensions">
                  <BlocksIcon />
                  Extensions
                </TabsTrigger>
              )}
              <TabsTrigger value="apps">
                <AppWindowIcon />
                Apps
              </TabsTrigger>
              {datastore.can_back_up && (
                <TabsTrigger value="backups">
                  <ArchiveIcon />
                  Backups
                </TabsTrigger>
              )}
              <TabsTrigger value="access">
                <NetworkIcon />
                Access
              </TabsTrigger>
              <TabsTrigger value="resources">
                <CpuIcon />
                Resources
              </TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="overview">
            <MetricsSection path={path} />
            <Connection datastore={datastore} />
          </TabsContent>
          {datastore.engine === "postgres" && (
            <TabsContent value="extensions">
              <Extensions datastore={datastore} onChanged={reload} />
            </TabsContent>
          )}
          <TabsContent value="apps">
            <Attachments datastore={datastore} onChanged={reload} />
          </TabsContent>
          {datastore.can_back_up && (
            <TabsContent value="backups">
              <DatabaseBackups datastore={datastore} />
            </TabsContent>
          )}
          <TabsContent value="access">
            <DatabaseAccess key={datastore.name} datastore={datastore} onChanged={reload} />
          </TabsContent>
          <TabsContent value="resources">
            <DatabaseLimits datastore={datastore} onSaved={setDatastore} />
          </TabsContent>
        </Tabs>
      )}
      {datastore && (
        <>
          <ConfirmDialog
            open={stopOpen}
            onOpenChange={setStopOpen}
            title={`Stop ${datastore.name}?`}
            description={
              <>
                The container stops and its data stays where it is — starting it again brings it
                back unchanged.{" "}
                {datastore.attachments.length > 0 ? (
                  <>
                    <strong>
                      {datastore.attachments.map((attachment) => attachment.app).join(", ")}
                    </strong>{" "}
                    will keep running and start failing to connect.
                  </>
                ) : (
                  "No app is attached to it."
                )}
              </>
            }
            confirmLabel="Stop it"
            onConfirm={() => power("stop")}
          />
          <ConfirmDialog
            open={deleteOpen}
            onOpenChange={setDeleteOpen}
            title={`Delete ${datastore.name}?`}
            description={
              <>
                The container is stopped and the data on disk goes with it. Nothing is kept, and{" "}
                {datastore.attachments.length > 0 ? (
                  <>
                    <strong>
                      {datastore.attachments.map((attachment) => attachment.app).join(", ")}
                    </strong>{" "}
                    will come up without a connection string the next time they are deployed.
                  </>
                ) : (
                  "no app is attached to it."
                )}
              </>
            }
            holdToConfirm
            confirmLabel="Delete database"
            onConfirm={async () => {
              await api.del(path);
              await queryClient.invalidateQueries({ queryKey: ["datastores"] });
              router.push("/databases");
            }}
          />
          <ContainerLogsDialog
            open={logsOpen}
            onOpenChange={setLogsOpen}
            path={path}
            name={datastore.name}
            hasContainer={datastore.has_container}
          />
        </>
      )}
    </>
  );
}

// The extensions this database has, and the ones it could have.
//
// Its own tab rather than a block on the create form: the form is
// already long enough to scroll on a laptop, and this is a decision
// somebody can make later — unlike the engine and the version, which
// they cannot.
//
// **Most of them interrupt nothing.** A contrib module is inside every
// Postgres image Cubeship runs, so installing one is a statement against
// the server that is up. Two are not, and only those warn: one needs a
// different image, the other needs its library loaded at startup, and
// both mean the container is replaced.
//
// **Nothing here removes one**, and the missing button is the point:
// removing means running an image without a library that a column's
// type, an index or a default may already need.
//
// A grid of small cards with a filter above it, and no pages. There are
// more than twenty and they are each a line long, so a list would be a
// screen of scrolling to reach the one somebody came for — and paging
// through a set this size is worse than either.
function Extensions({ datastore, onChanged }: { datastore: Datastore; onChanged: () => void }) {
  const [offered, setOffered] = useState<DatastoreEngineExtension[] | null>(null);
  const [query, setQuery] = useState("");
  const [asked, setAsked] = useState<DatastoreEngineExtension | null>(null);
  const [error, setError] = useState<string | null>(null);

  // From the daemon, because which extensions exist at which version is
  // the daemon's list — a copy out here would offer something it then
  // refuses.
  useEffect(() => {
    api
      .get<DatastoreEngine[]>("/datastores/engines")
      .then((list) => {
        const engine = list.find((e) => e.engine === datastore.engine);
        setOffered(
          (engine?.extensions ?? []).filter((x) => x.versions.includes(datastore.version)),
        );
      })
      .catch((e) => setError(message(e)));
  }, [datastore.engine, datastore.version]);

  const needle = query.trim().toLowerCase();
  // Over the name and the sentence beside it, because half of these are
  // known by what they do rather than by what they are called: "fuzzy"
  // should find pg_trgm.
  const matches = (offered ?? []).filter(
    (x) =>
      needle === "" ||
      x.name.includes(needle) ||
      x.sql_name.includes(needle) ||
      x.summary.toLowerCase().includes(needle),
  );
  const installed = matches.filter((x) => datastore.extensions.includes(x.name));
  const available = matches.filter((x) => !datastore.extensions.includes(x.name));
  // While the container is being replaced there is nothing useful to
  // click: a second install would race the first.
  const settling = datastore.status === "provisioning";

  function card(x: DatastoreEngineExtension, on: boolean) {
    return (
      <Card key={x.name} className="flex flex-col">
        <CardContent className="flex flex-1 flex-col gap-2">
          <div className="flex items-start justify-between gap-2">
            <span className="font-mono text-sm break-all text-foreground">{x.name}</span>
            {on ? (
              <span className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
                <CheckIcon className="size-3.5" />
                Installed
              </span>
            ) : (
              <Button
                variant="outline"
                size="xs"
                disabled={settling}
                title={settling ? "This database is being provisioned." : undefined}
                onClick={() => setAsked(x)}
              >
                Install
              </Button>
            )}
          </div>
          <p className="text-xs leading-relaxed text-subtle-foreground">{x.summary}</p>
          {!on && x.requires.length > 0 && (
            <p className="text-xs leading-relaxed text-subtle-foreground">
              Installs {x.requires.join(", ")} with it.
            </p>
          )}
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <SectionHeader
        title="Extensions"
        sub="Install PostgreSQL extensions. Some replace the container briefly; database files stay untouched."
      />

      {offered !== null && offered.length > 0 && (
        <SearchBar
          value={query}
          onChange={setQuery}
          placeholder="Filter extensions"
          className="mb-4"
          count={{ value: matches.length, singular: "extension", plural: "extensions" }}
        />
      )}

      <ErrorAlert error={error} />

      {offered === null && <LoadingList rows={3} />}

      {offered?.length === 0 && (
        <Notice>
          This release offers no extensions for {datastore.engine} {datastore.version}.
        </Notice>
      )}

      {offered !== null && offered.length > 0 && matches.length === 0 && (
        <Notice>Nothing matches “{query}”.</Notice>
      )}

      {installed.length > 0 && (
        <>
          <p className="mb-2 text-[11px] tracking-[0.12em] text-muted-foreground uppercase">
            Installed
          </p>
          <div className="mb-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {installed.map((x) => card(x, true))}
          </div>
        </>
      )}

      {available.length > 0 && (
        <>
          <p className="mb-2 text-[11px] tracking-[0.12em] text-muted-foreground uppercase">
            Available
          </p>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {available.map((x) => card(x, false))}
          </div>
        </>
      )}

      {/* A deliberate second click and no word to type. It is not
          destructive — the data is not touched — and for most of these
          nothing even pauses, so the dialog says which kind this is
          rather than warning about downtime that is not coming. */}
      <ConfirmDialog
        open={asked !== null}
        onOpenChange={(v) => !v && setAsked(null)}
        title={`Install ${asked?.name ?? ""}?`}
        description={
          asked?.builtin ? (
            <>
              It is already inside this database&apos;s image, so this creates it and nothing else —{" "}
              <code className="text-foreground">{datastore.name}</code> keeps answering throughout.
              It cannot be removed afterwards.
            </>
          ) : (
            <>
              This replaces <code className="text-foreground">{datastore.name}</code>&apos;s
              container, so it stops answering for a few seconds. Its data is a directory on the
              host and is not touched. It cannot be removed afterwards.
            </>
          )
        }
        confirmLabel="Install"
        onConfirm={async () => {
          if (!asked) return;
          await api.post(`${datastorePath(datastore.name)}/extensions`, {
            extensions: [asked.name],
          });
          setAsked(null);
          onChanged();
        }}
      />
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
    <Card className="mb-4">
      <CardContent className="space-y-5">
        <header className="flex flex-wrap items-start justify-between gap-3">
          <div className="space-y-1">
            <h2 className="text-lg font-medium tracking-tight">Connection settings</h2>
            <p className="max-w-2xl text-sm text-muted-foreground">
              Apps on this instance reach it by container name on the shared network.
            </p>
          </div>
          {creds && (
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
          )}
        </header>

        <ErrorAlert error={error} />
        {forbidden && (
          <Notice>The password and connection strings are available to administrators only.</Notice>
        )}

        <div className="grid gap-4 sm:grid-cols-2">
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
        </div>
      </CardContent>
    </Card>
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
