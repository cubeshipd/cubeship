"use client";

import { ChevronLeftIcon, EyeIcon, EyeOffIcon, RefreshCwIcon, SettingsIcon } from "lucide-react";
import Link from "next/link";
import { use, useCallback, useEffect, useState } from "react";
import { CopyButton } from "@/components/copy-button";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { MetricsSection } from "@/components/metrics-section";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { ValueCard } from "@/components/value-card";
import {
  type App,
  api,
  BUILDING_SOURCES,
  type Deployment,
  type EnvView,
  type ResolvedVar,
} from "@/lib/api";
import { message } from "@/lib/errors";

// One app, under the environment it lives in — which is the only place
// it means anything. `gateway` is unique in acme/api/production and
// nowhere else, so the URL is the reference and the reference is the
// URL.
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
        }
      />

      {!app && <LoadingList rows={5} />}

      {app && (
        <>
          <Origin app={app} />

          {/* After what the app is made of and before how it has been
          deployed: the first says which app this is, and this says how
          it is doing right now. Same component and same daemon module
          the databases use — an app and a database are one question
          about two kinds of container. */}
          <MetricsSection path={path} />

          <Deployments
            reference={reference}
            buildsFromRepo={BUILDING_SOURCES.includes(app.source)}
            onDeployed={reload}
          />
          <EnvVars reference={reference} />
          <Logs reference={reference} />
        </>
      )}
    </>
  );
}

// Where this app's image comes from, and therefore what you do to
// deploy it: push to it, name a tag, or point it at a commit.
function Origin({ app }: { app: App }) {
  if (app.source === "dockerfile" || app.source === "railpack") {
    return (
      <>
        <ValueCard
          label={
            app.source === "dockerfile" ? "Built from the Dockerfile in" : "Built by Railpack from"
          }
          value={app.repo}
        />
        <div className="mb-4 grid gap-3 sm:grid-cols-2">
          <ValueCard className="mb-0" label="Default branch or commit" value={app.ref || "—"} />
          {app.source === "dockerfile" && (
            <ValueCard className="mb-0" label="Dockerfile" value={app.dockerfile || "Dockerfile"} />
          )}
        </div>
        <Notice>
          Nothing tells Cubeship when the repository changes, so a deploy is something you ask for.
          A deploy can name any branch or commit; this is what it falls back to.
        </Notice>
      </>
    );
  }

  if (app.source === "external") {
    return (
      <ValueCard
        label={
          <>
            Pulls from another registry. Nothing tells Cubeship when it is pushed to, so it deploys
            when you ask — add a login under{" "}
            <Link href="/registries" className="underline underline-offset-4">
              Registries
            </Link>{" "}
            if it is private.
          </>
        }
        value={app.image}
      />
    );
  }

  // An app that pushes to Cubeship's own registry says nothing here.
  // Where to push is one line somebody needs once, when they wire the
  // repository up, and a banner carrying it sat at the top of the page
  // for the rest of the app's life.
  if (app.image) {
    return null;
  }

  return (
    <Notice tone="warning">
      There is nowhere to push yet — this instance has no domain. Set one under{" "}
      <Link href="/settings" className="underline underline-offset-4">
        Instance
      </Link>
      .
    </Notice>
  );
}

function Deployments({
  reference,
  buildsFromRepo,
  onDeployed,
}: {
  reference: string;
  // A build takes a branch or a commit; an image takes a tag. Same
  // field, same endpoint, two different things to type into it.
  buildsFromRepo: boolean;
  onDeployed: () => void;
}) {
  const [list, setList] = useState<Deployment[] | null>(null);
  const [tag, setTag] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const path = `/apps/${reference}/deployments`;
  const reload = useCallback(() => {
    api
      .get<Deployment[]>(path)
      .then(setList)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  // A deploy runs detached, so the row is the only place its outcome
  // appears. Poll while one is in flight and stop when none is.
  useEffect(() => {
    if (!list?.some((d) => d.status === "pending" || d.status === "deploying")) return;
    const timer = setInterval(() => {
      reload();
      onDeployed();
    }, 2000);
    return () => clearInterval(timer);
  }, [list, reload, onDeployed]);

  return (
    <>
      <SectionHeader
        title="Deployments"
        actions={
          <form
            className="flex items-center gap-2"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              setError(null);
              try {
                await api.post(`/apps/${reference}/deploy`, { tag: tag.trim() });
                reload();
              } catch (err) {
                setError(message(err));
              }
              setBusy(false);
            }}
          >
            <Input
              value={tag}
              onChange={(e) => setTag(e.target.value)}
              placeholder={buildsFromRepo ? "main" : "latest"}
              aria-label={buildsFromRepo ? "Branch or commit to deploy" : "Tag to deploy"}
              className="w-32 text-xs"
            />
            <Button type="submit" variant="outline" disabled={busy}>
              Deploy
            </Button>
          </form>
        }
      />
      <ErrorAlert error={error} />

      <Card className="py-0">
        <Table>
          <TableBody>
            {list?.map((d) => (
              <TableRow key={d.id}>
                <TableCell className="px-4 py-2.5">
                  <StatusBadge value={d.status} />
                </TableCell>
                <TableCell className="px-4 py-2.5 font-mono text-xs break-all text-muted-foreground">
                  {d.image}
                </TableCell>
                <TableCell className="px-4 py-2.5 text-xs whitespace-nowrap text-muted-foreground">
                  {new Date(d.created_at).toLocaleString()}
                </TableCell>
                {d.error && (
                  <TableCell className="px-4 py-2.5 text-xs text-destructive">{d.error}</TableCell>
                )}
              </TableRow>
            ))}
            {list?.length === 0 && (
              <TableRow className="hover:bg-transparent">
                <TableCell className="px-4 py-3 text-sm text-muted-foreground">
                  Nothing deployed yet.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>
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

function EnvVars({ reference }: { reference: string }) {
  const [view, setView] = useState<EnvView | null>(null);
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);

  const path = `/apps/${reference}/env`;
  const reload = useCallback(() => {
    api
      .get<EnvView>(path)
      .then(setView)
      .catch((e) => setError(message(e)));
  }, [path]);
  useEffect(reload, [reload]);

  async function unset(name: string) {
    setError(null);
    try {
      await api.patch(path, { unset: [name] });
      reload();
    } catch (e) {
      setError(message(e));
    }
  }

  const columns: Column<ResolvedVar>[] = [
    {
      id: "key",
      header: "Name",
      width: 30,
      sortBy: (v) => v.key,
      cell: (v) => <span className="font-mono text-xs">{v.key}</span>,
    },
    {
      id: "value",
      header: "Value",
      width: 46,
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
      id: "unset",
      header: "",
      width: 10,
      align: "right",
      cell: (v) =>
        // Only what this app set itself. A variable inherited from the
        // project or the environment is not this screen's to remove —
        // unsetting it here would look like it worked and change
        // nothing.
        v.source === "app" ? (
          <Button
            variant="ghost"
            size="xs"
            className="text-muted-foreground hover:text-destructive"
            onClick={() => unset(v.key)}
          >
            Unset
          </Button>
        ) : null,
    },
  ];

  return (
    <>
      <SectionHeader
        title="Environment"
        sub="The project's variables, then the environment's, then any database attached to this app, then the app's own. Setting one here overrides the levels above it."
      />
      <ErrorAlert error={error} />

      <DataTable
        columns={columns}
        rows={view?.effective ?? null}
        rowKey={(v) => v.key}
        loadingRows={3}
        empty="No variables."
        className="mb-3"
      />

      <form
        className="flex items-center gap-2"
        onSubmit={async (e) => {
          e.preventDefault();
          setError(null);
          try {
            await api.patch(path, { set: { [key]: value } });
            setKey("");
            setValue("");
            reload();
          } catch (err) {
            setError(message(err));
          }
        }}
      >
        <Input
          value={key}
          onChange={(e) => setKey(e.target.value)}
          placeholder="KEY"
          aria-label="Variable name"
          className="w-56 text-xs"
        />
        <Input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="value"
          aria-label="Variable value"
          className="flex-1 text-xs"
        />
        <Button type="submit" variant="outline" disabled={!key}>
          Set
        </Button>
      </form>
    </>
  );
}

function Logs({ reference }: { reference: string }) {
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Logs come back as plain text, not JSON, so they bypass the client.
  const load = useCallback(async () => {
    setError(null);
    try {
      const res = await fetch(`/api/apps/${reference}/logs?tail=200`, {
        credentials: "same-origin",
      });
      const body = await res.text();
      if (!res.ok) throw new Error(body.trim() || res.statusText);
      setText(body);
    } catch (e) {
      setError(message(e));
    }
  }, [reference]);

  return (
    <>
      <SectionHeader
        title="Logs"
        actions={
          <Button variant="outline" onClick={load}>
            <RefreshCwIcon />
            {text === null ? "Load" : "Refresh"}
          </Button>
        }
      />
      <ErrorAlert error={error} />
      {text !== null && (
        <pre className="max-h-[420px] overflow-auto border border-border bg-black p-3 font-mono text-xs break-all whitespace-pre-wrap text-success/90">
          {text || "(no output)"}
        </pre>
      )}
    </>
  );
}
