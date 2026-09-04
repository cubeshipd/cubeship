"use client";

import { ChevronLeftIcon, RefreshCwIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { Shell } from "@/components/shell";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { ValueCard } from "@/components/value-card";
import { type App, api, type Deployment, type EnvView } from "@/lib/api";
import { message } from "@/lib/errors";

// One app, identified by its reference in the query string. A static
// export has no dynamic segments, and the reference is four path
// components anyway — carrying it as one value keeps it whole.
export default function AppPage() {
  return (
    <Shell>
      <Suspense>
        <Detail />
      </Suspense>
    </Shell>
  );
}

function Detail() {
  const router = useRouter();
  const reference = useSearchParams().get("ref") ?? "";
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

  // Where this app came from, and where deleting it returns to.
  const environment = `/projects?ref=${app.org}/${app.project}&env=${app.environment}`;

  return (
    <>
      <Link
        href={environment}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {app.org}/{app.project}/{app.environment}
      </Link>

      <PageHeader
        title={<span className="font-mono text-lg tracking-normal normal-case">{app.name}</span>}
        sub={
          <span className="flex items-center gap-2">
            {app.domain || "no domain"}
            <StatusBadge value={app.status} />
          </span>
        }
        actions={
          <Button
            variant="destructive"
            onClick={async () => {
              try {
                await api.del(path);
                router.push(environment);
              } catch (e) {
                setError(message(e));
              }
            }}
          >
            <Trash2Icon />
            Delete app
          </Button>
        }
      />

      {app.source === "external" ? (
        <ValueCard
          label={
            <>
              Pulls from another registry. Nothing tells Cubeship when it is pushed to, so it
              deploys when you ask — add a login under{" "}
              <Link href="/registries" className="underline underline-offset-4">
                Registries
              </Link>{" "}
              if it is private.
            </>
          }
          value={app.image}
        />
      ) : app.image ? (
        <ValueCard
          label="Push an image here and it deploys"
          value={`docker push ${app.image}:latest`}
        />
      ) : (
        <Notice tone="warning">
          There is nowhere to push yet — this instance has no domain. Set one under{" "}
          <Link href="/settings" className="underline underline-offset-4">
            Instance
          </Link>
          .
        </Notice>
      )}

      <Deployments reference={reference} onDeployed={reload} />
      <EnvVars reference={reference} />
      <Logs reference={reference} />
    </>
  );
}

function Deployments({ reference, onDeployed }: { reference: string; onDeployed: () => void }) {
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
              placeholder="latest"
              aria-label="Tag to deploy"
              className="w-32 font-mono text-xs"
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
          className="w-56 font-mono text-xs"
        />
        <Input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="value"
          aria-label="Variable value"
          className="flex-1 font-mono text-xs"
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
