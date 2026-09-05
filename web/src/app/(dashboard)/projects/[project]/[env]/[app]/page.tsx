"use client";

import { ChevronLeftIcon, RefreshCwIcon, SettingsIcon } from "lucide-react";
import Link from "next/link";
import { use, useCallback, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { ValueCard } from "@/components/value-card";
import { type App, api, BUILDING_SOURCES, type Deployment, type EnvView, hostsOf } from "@/lib/api";
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
  return <Detail reference={`${project}/${env}/${app}`} />;
}

function Detail({ reference }: { reference: string }) {
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
  if (!app) return null;

  // Where this app came from.
  const environment = `/projects/${app.project}/${app.environment}`;

  return (
    <>
      <Link
        href={environment}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {app.project}/{app.environment}
      </Link>

      <PageHeader
        title={<span className="font-mono text-lg tracking-normal normal-case">{app.name}</span>}
        sub={
          <span className="flex items-center gap-2">
            {hostsOf(app)}
            <StatusBadge value={app.status} />
          </span>
        }
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

      {app.domains.length === 0 && (
        <Notice>
          This app answers at no name, so only its neighbours on this instance reach it — by
          container name. That is all a worker needs; anything meant to be visited wants a domain,
          in{" "}
          <Link href={`/projects/${reference}/settings`} className="underline underline-offset-4">
            settings
          </Link>
          .
        </Notice>
      )}

      <Origin app={app} />

      <Deployments
        reference={reference}
        buildsFromRepo={BUILDING_SOURCES.includes(app.source)}
        onDeployed={reload}
      />
      <EnvVars reference={reference} />
      <Logs reference={reference} />
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

  if (app.image) {
    return (
      <ValueCard
        label="Push an image here and it deploys"
        value={`docker push ${app.image}:latest`}
      />
    );
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

  return (
    <>
      <SectionHeader
        title="Environment"
        sub="The project's variables, then the environment's, then the app's own. Setting one here overrides the levels above it."
      />
      <ErrorAlert error={error} />

      <Card className="mb-3 py-0">
        <Table>
          <TableBody>
            {view?.effective?.map((v) => (
              <TableRow key={v.key}>
                <TableCell className="px-4 py-2.5 font-mono text-xs">{v.key}</TableCell>
                <TableCell className="px-4 py-2.5 font-mono text-xs break-all">{v.value}</TableCell>
                <TableCell className="px-4 py-2.5 text-right text-xs text-muted-foreground">
                  {v.source}
                  {v.source === "app" && (
                    <Button
                      variant="ghost"
                      size="xs"
                      className="ml-2 text-muted-foreground hover:text-destructive"
                      onClick={async () => {
                        try {
                          await api.patch(path, { unset: [v.key] });
                          reload();
                        } catch (e) {
                          setError(message(e));
                        }
                      }}
                    >
                      Unset
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
            {view?.effective?.length === 0 && (
              <TableRow className="hover:bg-transparent">
                <TableCell className="px-4 py-3 text-sm text-muted-foreground">
                  No variables.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>

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
        <Button type="submit" variant="outline">
          Set
        </Button>
      </form>
      <p className="mt-2 text-xs text-subtle-foreground">Takes effect on the next deploy.</p>
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
