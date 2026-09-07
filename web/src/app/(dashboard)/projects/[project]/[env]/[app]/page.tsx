"use client";

import { cn } from "cn";
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
import { use, useCallback, useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContainerLogs } from "@/components/container-logs";
import { CopyButton } from "@/components/copy-button";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { MetricsSection } from "@/components/metrics-section";
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
        <Link href="/" className="text-foreground underline underline-offset-4">
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
          <TabsList className="mb-6">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="environment">Environment</TabsTrigger>
            <TabsTrigger value="logs">Logs</TabsTrigger>
          </TabsList>

          <TabsContent value="overview">
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
      width: 62,
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
      width: 22,
      sortBy: (d) => d.created_at,
      cell: (d) => (
        <span className="text-xs text-muted-foreground">
          {new Date(d.created_at).toLocaleString()}
        </span>
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
      />
    </>
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
  const [mineOnly, setMineOnly] = useState(false);
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
    return all.filter(
      (v) => (!mineOnly || v.source === "app") && (!needle || v.key.toLowerCase().includes(needle)),
    );
  }, [all, filter, mineOnly]);

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

      <div className="mb-3 flex items-center gap-2">
        <SearchBar
          value={filter}
          onChange={setFilter}
          placeholder="Filter by name"
          className="min-w-0 flex-1"
          trailing={
            all && rows ? (
              <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
                {rows.length === all.length ? all.length : `${rows.length}/${all.length}`}
              </span>
            ) : undefined
          }
        />
        {/* Most of a long list is inherited, and what somebody came here
            to change is the part this app set itself. */}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-pressed={mineOnly}
          onClick={() => setMineOnly(!mineOnly)}
          className={cn(mineOnly && "bg-secondary text-foreground")}
        >
          Set here
        </Button>
      </div>

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(v) => v.key}
        loadingRows={6}
        maxHeight="60vh"
        empty={filter.trim() || mineOnly ? "Nothing matches." : "No variables."}
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
