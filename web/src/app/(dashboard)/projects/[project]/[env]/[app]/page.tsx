"use client";

import {
  ChevronLeftIcon,
  EyeIcon,
  EyeOffIcon,
  PencilIcon,
  PlusIcon,
  RocketIcon,
  SettingsIcon,
  Trash2Icon,
} from "lucide-react";
import Link from "next/link";
import { use, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogs, LogView } from "@/components/container-logs";
import { CopyButton } from "@/components/copy-button";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { MetricsSection } from "@/components/metrics-section";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchBar } from "@/components/search-bar";
import { StatusBadge } from "@/components/status-badge";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { type App, api, type Deployment, type EnvView, type ResolvedVar } from "@/lib/api";
import { message } from "@/lib/errors";

// One app, under the environment it lives in — which is the only place
// it means anything. `gateway` is unique in acme/api/production and
// nowhere else, so the URL is the reference and the reference is the
// URL.
//
// **Tabs, not one column of sections.** The databases' page argues the
// other way and is right about itself: three short sections that are not
// alternatives, all worth seeing at once. An app is not that. Its
// environment is fifty rows on a real app and its log is five thousand
// lines, and stacked they push everything else off the screen — so
// "how is it doing" stops being visible the moment an app is big enough
// to care about. Each tab here is a question somebody arrives with:
// how is it running, how is it configured, what is it saying.
//
// PageProps comes from Next's generated route types, so the params this
// destructures are the segments the directory actually has. Spelling one
// that is not there is a build error rather than an `undefined` glued
// into the reference — which is how this page once asked the daemon for
// /apps/undefined/<project>/<env>/<app> and got "no such endpoint".
export default function AppPage({ params }: PageProps<"/projects/[project]/[env]/[app]">) {
  const { project, env, app } = use(params);
  // The three segments travel separately as well as joined: they are
  // what lets the page draw its own heading before the daemon has said
  // anything, which is the difference between a navigation that feels
  // instant and one that blinks.
  return <Detail reference={`${project}/${env}/${app}`} project={project} env={env} name={app} />;
}

type Tab = "overview" | "environment" | "logs";

// The one reason a deploy's record cannot be removed: the daemon is
// still writing to it.
const stillRunning = "This deploy is still running. Wait for it to finish.";

// Why the Logs tab is dead. Said on hover, because a disabled control
// that explains nothing is a control somebody clicks twice.
const noContainer = "Nothing has run yet, so there is no log. Deploy the app first.";

// The other reason the Logs tab is dead: the log is on another machine.
// The daemon refuses it in the same words rather than answering with
// this machine's Docker saying "no such container", which is what it
// would say.
const elsewhere = "This app runs on another server, and its log is not readable from here yet.";

function Detail({
  reference,
  project,
  env,
  name,
}: {
  reference: string;
  project: string;
  env: string;
  name: string;
}) {
  const [app, setApp] = useState<App | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("overview");
  // Bumped by the header's Deploy button, which is not inside the
  // section that has to react to it.
  const [deployed, setDeployed] = useState(0);

  const path = `/apps/${reference}`;
  const reload = useCallback(() => {
    if (!reference) return;
    api
      .get<App>(path)
      .then(setApp)
      .catch((e) => setError(message(e)));
  }, [path, reference]);
  useEffect(reload, [reload]);

  if (!reference) {
    return (
      <p className="text-sm text-muted-foreground">
        No app named.{" "}
        <Link href="/projects" className="text-foreground underline underline-offset-4">
          Back to projects
        </Link>
        .
      </p>
    );
  }
  if (error) return <ErrorAlert error={error} />;

  // Where this app came from. Built from the URL rather than from the
  // answer, so the way back is there before the answer is.
  const environment = `/projects/${project}/${env}`;

  // Whether this app runs on another machine, which changes what this
  // screen can show: its log and its charts are that machine's.
  const remote = app !== null && app.node !== "control-plane";

  return (
    <>
      <Link
        href={environment}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {project}/{env}
      </Link>

      {/* Drawn from the URL, so it is on screen the moment you navigate
          here. This used to return null until the app arrived, which
          blanked the content area on every route change and then
          filled it — a page that disappears before it appears reads as
          slow however fast the request was. */}
      <PageHeader
        title={<span className="font-mono text-lg tracking-normal normal-case">{name}</span>}
        actions={
          <>
            <DeployButton
              reference={reference}
              onDeployed={() => {
                setDeployed((n) => n + 1);
                // Deploying from the header is worth watching, and what
                // there is to watch is on the first tab.
                setTab("overview");
                reload();
              }}
            />
            <Button
              variant="outline"
              nativeButton={false}
              render={
                <Link href={`/projects/${reference}/settings`}>
                  <SettingsIcon />
                  Settings
                </Link>
              }
            />
          </>
        }
      />

      {!app && <LoadingList rows={5} />}

      {app && (
        <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
          {/* The line variant: labels on a rule with the live one lit,
              rather than a filled box that draws more of itself than
              what it switches. See globals.css. */}
          <TabsList variant="line">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="environment">Environment</TabsTrigger>
            {/* Nothing has printed anything until something has run.
                The daemon refuses this endpoint with a 409 in that
                state, and a tab whose whole content is that refusal is
                a tab that should not have been offered. */}
            <TabsTrigger
              value="logs"
              disabled={!app.has_container || remote}
              title={remote ? elsewhere : app.has_container ? undefined : noContainer}
            >
              Logs
            </TabsTrigger>
          </TabsList>

          <TabsContent value="overview">
            {/* Where it runs, said once and only when it is somewhere
                else. It also explains the empty charts under it: what
                samples a container is the daemon on the machine it is
                on, and only this one writes to the series. */}
            {remote && (
              <Notice>
                This app runs on <code className="text-foreground">{app.node}</code>. Its charts and
                its log are on that machine — <code>docker ps</code> there finds it.
              </Notice>
            )}
            <MetricsSection path={path} />
            <Deployments reference={reference} deployed={deployed} onSettled={reload} />
          </TabsContent>

          <TabsContent value="environment">
            <EnvVars reference={reference} />
          </TabsContent>

          <TabsContent value="logs">
            <ContainerLogs path={path} title={null} tall />
          </TabsContent>
        </Tabs>
      )}
    </>
  );
}

// Deploying, from the header, with nothing to fill in.
//
// **No tag field.** What a deploy runs is the app's source and the ref
// it is configured with — a branch for something built here, a tag for
// an image — and that is a decision with consequences, made in the
// app's settings where it is written down and stays written down.
// Typing a branch beside the button made every deploy a fresh decision
// about which code this app is, taken in the moment and remembered
// nowhere.
function DeployButton({ reference, onDeployed }: { reference: string; onDeployed: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  return (
    <>
      <ActionButton
        busy={busy}
        onClick={async () => {
          setBusy(true);
          setError(null);
          try {
            await api.post(`/apps/${reference}/deploy`, {});
            onDeployed();
          } catch (err) {
            setError(message(err));
          }
          setBusy(false);
        }}
      >
        <RocketIcon />
        Deploy
      </ActionButton>
      {/* In the header, because that is where the button is. An error
          from it below the fold is an error nobody connects to the
          thing they just pressed. */}
      {error && <span className="max-w-md text-xs leading-relaxed text-destructive">{error}</span>}
    </>
  );
}

function Deployments({
  reference,
  deployed,
  onSettled,
}: {
  reference: string;
  // Bumped when the header deploys, which is the one thing that changes
  // this list from outside it.
  deployed: number;
  onSettled: () => void;
}) {
  const [list, setList] = useState<Deployment[] | null>(null);
  const [open, setOpen] = useState<Deployment | null>(null);
  const [deleting, setDeleting] = useState<Deployment | null>(null);
  // The app's own name, which is what the confirmation asks somebody to
  // type before it takes the app off the air.
  const name = reference.split("/").pop() ?? reference;
  const [error, setError] = useState<string | null>(null);

  const path = `/apps/${reference}/deployments`;
  const reload = useCallback(() => {
    api
      .get<Deployment[]>(path)
      .then(setList)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // The header's Deploy button is outside this section, so a bump is
  // how it says there is a new row to go and fetch.
  useEffect(() => {
    if (deployed === 0) return;
    reload();
  }, [deployed, reload]);

  // A deploy runs detached, so the row is the only place its outcome
  // appears. Poll while one is in flight and stop when none is.
  useEffect(() => {
    if (!list?.some((d) => d.status === "pending" || d.status === "deploying")) return;
    const timer = setInterval(() => {
      reload();
      onSettled();
    }, 2000);
    return () => clearInterval(timer);
  }, [list, reload, onSettled]);

  const columns: Column<Deployment>[] = [
    {
      id: "status",
      header: "Status",
      width: 16,
      sortBy: (d) => d.status,
      cell: (d) => <StatusBadge value={d.status} />,
    },
    {
      id: "image",
      header: "Image",
      width: 56,
      // wrap, and the error under it in the same cell. A build's
      // failure is a paragraph: given a column of its own it made the
      // table wider than the page and put a horizontal scrollbar under
      // everything, which is how the one thing worth reading ended up
      // being the one thing off screen.
      wrap: true,
      cell: (d) => (
        <>
          <div className="font-mono text-xs break-all text-muted-foreground">{d.image}</div>
          {d.error && (
            <div className="mt-1.5 text-xs leading-relaxed break-words text-destructive">
              {d.error}
            </div>
          )}
        </>
      ),
    },
    {
      id: "when",
      header: "When",
      width: 20,
      sortBy: (d) => d.created_at,
      cell: (d) => (
        <span className="text-xs text-muted-foreground">
          {new Date(d.created_at).toLocaleString()}
        </span>
      ),
    },
    {
      id: "actions",
      header: "",
      width: 8,
      align: "right",
      // Disabled with the reason rather than hidden: a missing button
      // explains nothing, and "why can I not clear this one" is the
      // question somebody has while looking straight at it.
      cell: (d) => (
        <RowActions>
          <RowAction
            icon={Trash2Icon}
            label={d.live ? `Take the app down` : `Delete deploy ${d.id}`}
            danger={d.deletable}
            disabled={!d.deletable}
            title={
              d.deletable
                ? d.live
                  ? "This is what the app is running. Deleting it takes the app down."
                  : undefined
                : stillRunning
            }
            onClick={() => setDeleting(d)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <SectionHeader title="Deployments" />
      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={list}
        rowKey={(d) => String(d.id)}
        loadingRows={3}
        maxHeight="50vh"
        empty="Nothing deployed yet."
        className="mb-4"
        // Every row opens, not only the ones that built something. What
        // is behind it is the deploy — what ran, when, why it stopped
        // and what it printed — and a row that opened for some deploys
        // and not others would be a table you have to guess at.
        onRowClick={setOpen}
      />

      <DeploymentDialog
        reference={reference}
        deployment={open}
        onOpenChange={(shown) => !shown && setOpen(null)}
      />

      {/* Two confirmations, because these are two different acts. An
          old row is history and a second click is guard enough; the
          live one takes the app off the air, and that asks for the
          app's own name — the house rule for anything a misclick must
          not reach. */}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(shown) => !shown && setDeleting(null)}
        title={deleting?.live ? "Take the app down?" : "Delete this record?"}
        confirmLabel={deleting?.live ? "Take it down" : "Delete"}
        confirmWord={deleting?.live ? name : undefined}
        description={
          deleting?.live ? (
            <>
              This is what <code className="text-foreground">{name}</code> is running. Its container
              is stopped and removed, and the app stops answering.{" "}
              <strong>The app itself stays</strong> — its domains, its environment and everything
              attached to it — and the next deploy brings it back.
            </>
          ) : (
            <>
              The row goes, and the build log with it. <strong>The app is not touched</strong> —
              this is the history of a deploy, not anything running now.
            </>
          )
        }
        onConfirm={async () => {
          if (!deleting) return;
          await api.del(`${path}/${deleting.id}`);
          setDeleting(null);
          reload();
          onSettled();
        }}
      />
    </>
  );
}

// One deploy, opened.
//
// It fetches the deployment again rather than showing the row it was
// opened from, because the row does not carry the log: a build's output
// is capped at 256 KiB and the listing is fifty rows, so the daemon
// hands it over one deployment at a time. While the deploy is still
// running it asks again on a timer, which is what makes this the place
// to watch a build from.
function DeploymentDialog({
  reference,
  deployment,
  onOpenChange,
}: {
  reference: string;
  // The row that was clicked, or null when nothing is open.
  deployment: Deployment | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [full, setFull] = useState<Deployment | null>(null);
  const [error, setError] = useState<string | null>(null);

  const id = deployment?.id;
  const load = useCallback(() => {
    if (!id) return;
    api
      .get<Deployment>(`/apps/${reference}/deployments/${id}`)
      .then((d) => {
        setFull(d);
        setError(null);
      })
      .catch((e) => setError(message(e)));
  }, [reference, id]);

  useEffect(() => {
    setFull(null);
    load();
  }, [load]);

  // A build writes its output as it goes, so this is the one screen
  // where re-reading under somebody is the point rather than a
  // discourtesy — and it stops the moment the deploy settles.
  const running = (full ?? deployment)?.status === "pending";
  useEffect(() => {
    if (!id || !running) return;
    const timer = setInterval(load, 2000);
    return () => clearInterval(timer);
  }, [id, running, load]);

  const shown = full ?? deployment;

  // Focus lands on the panel rather than on the first thing in it.
  // The first thing in it is the log's filter, and a dialog opened to
  // read a build's output should not open with a cursor blinking in a
  // field: it is what a dialog that wants typing looks like, and here
  // there is nothing to type until somebody has read something.
  const panel = useRef<HTMLDivElement>(null);

  return (
    <Dialog open={deployment !== null} onOpenChange={onOpenChange}>
      <DialogContent ref={panel} initialFocus={panel} className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-3">
            Deploy
            {shown && <StatusBadge value={shown.status} />}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-3 py-4">
          <ErrorAlert error={error} />

          {shown && (
            <div className="flex flex-wrap items-center gap-x-6 gap-y-1 font-mono text-xs text-muted-foreground">
              <span className="break-all">{shown.image || "—"}</span>
              <span>{new Date(shown.created_at).toLocaleString()}</span>
            </div>
          )}

          {shown?.error && (
            <div className="border-l-2 border-destructive bg-destructive/8 px-3 py-2 text-xs leading-relaxed break-words text-destructive">
              {shown.error}
            </div>
          )}

          <LogView
            text={full?.logs ?? null}
            busy={full === null}
            name={`${reference.split("/").pop()}-deploy-${deployment?.id ?? ""}`}
            follow={running}
            empty={
              running
                ? "Waiting for the build to say something."
                : "This deploy printed nothing — it ran an image somebody else built rather than building one."
            }
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}

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
// Built for fifty of them rather than five, which is what a real app
// has. Three things follow from that and none of them was true before:
// there is a filter, because scrolling fifty rows to find one is not
// reading; the list is bounded and scrolls inside itself, because
// otherwise the list *is* the page; and adding one is a dialog at the
// top rather than a pair of fields under the fiftieth row, which is a
// scroll away from wherever you were.
function EnvVars({ reference }: { reference: string }) {
  const [view, setView] = useState<EnvView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
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
  const rows = useMemo(() => {
    if (all === null) return null;
    const needle = filter.trim().toLowerCase();
    if (!needle) return all;
    return all.filter((v) => v.key.toLowerCase().includes(needle));
  }, [all, filter]);

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
      cell: (v) => <span className="text-xs text-muted-foreground">{v.source}</span>,
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
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            <PlusIcon />
            Add variable
          </Button>
        }
      />
      <ErrorAlert error={error} />

      {/* The field and nothing beside it. There was a "Set here" toggle
          here, for the app's own variables — but the table already says
          which level set each row, and a filter with a second control
          attached asks somebody to work out how the two combine before
          typing the name they came with. */}
      <SearchBar
        value={filter}
        onChange={setFilter}
        placeholder="Filter by name"
        className="mb-3"
        trailing={
          all && rows ? (
            <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
              {rows.length === all.length ? all.length : `${rows.length}/${all.length}`}
            </span>
          ) : undefined
        }
      />

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(v) => v.key}
        loadingRows={6}
        maxHeight="60vh"
        empty={filter.trim() ? "Nothing matches." : "No variables."}
        className="mb-4"
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
