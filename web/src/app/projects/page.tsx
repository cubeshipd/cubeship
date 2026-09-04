"use client";

import { PlusIcon, SettingsIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AppCard } from "@/components/app-card";
import { ErrorAlert } from "@/components/error-alert";
import { useOrg } from "@/components/org-context";
import { PageHeader } from "@/components/page-header";
import { Shell } from "@/components/shell";
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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { type App, api, type Environment } from "@/lib/api";
import { message } from "@/lib/errors";
import { sanitize } from "@/lib/slug";

// One project, opened on an environment. A static export has no dynamic
// segments, so the project travels in the query string as org/project —
// whole, and self-contained enough that the link works for someone whose
// sidebar is pointing at another organization.
export default function ProjectPage() {
  return (
    <Shell>
      <Suspense>
        <Detail />
      </Suspense>
    </Shell>
  );
}

// production is the environment a project always has and cannot lose,
// so it is where the project opens.
const DEFAULT_ENV = "production";

function Detail() {
  const router = useRouter();
  const params = useSearchParams();
  const { org: selected, select } = useOrg();

  const ref = params.get("ref") ?? "";
  const [org, project] = ref.split("/");
  const wanted = params.get("env") ?? "";

  const [envs, setEnvs] = useState<Environment[] | null>(null);
  const [apps, setApps] = useState<App[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Following a link into another organization moves the whole
  // dashboard there, rather than showing one page out of frame.
  useEffect(() => {
    if (org && selected && org !== selected) select(org);
  }, [org, selected, select]);

  const path = `/orgs/${org}/projects/${project}`;
  const reloadEnvs = useCallback(() => {
    if (!org || !project) return;
    api
      .get<Environment[]>(`${path}/environments`)
      .then(setEnvs)
      .catch((e) => setError(message(e)));
  }, [path, org, project]);
  useEffect(reloadEnvs, [reloadEnvs]);

  const reloadApps = useCallback(() => {
    api
      .get<App[]>("/apps")
      .then(setApps)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reloadApps, [reloadApps]);

  if (!ref || !project) {
    return (
      <p className="text-sm text-muted-foreground">
        No project named.{" "}
        <Link href="/" className="text-foreground underline underline-offset-4">
          Back to projects
        </Link>
        .
      </p>
    );
  }

  const known = envs?.map((e) => e.slug) ?? [];
  const env = known.includes(wanted)
    ? wanted
    : known.includes(DEFAULT_ENV)
      ? DEFAULT_ENV
      : (known[0] ?? "");

  const shown = apps?.filter(
    (a) => a.org === org && a.project === project && a.environment === env,
  );

  function goTo(next: string) {
    router.replace(`/projects?ref=${ref}&env=${next}`, { scroll: false });
  }

  return (
    <>
      <PageHeader
        title={project}
        sub={
          <span className="font-mono text-xs">
            {org}/{project}
          </span>
        }
        actions={
          <>
            <Button
              nativeButton={false}
              render={
                <Link href={`/apps/new?project=${project}&env=${env}`}>
                  <PlusIcon />
                  New app
                </Link>
              }
            />
            <Button
              variant="outline"
              nativeButton={false}
              render={
                <Link href={`/projects/settings?ref=${ref}`}>
                  <SettingsIcon />
                  Settings
                </Link>
              }
            />
          </>
        }
      />

      <ErrorAlert error={error} />

      <div className="mb-5 flex items-center gap-2">
        <Tabs value={env} onValueChange={(v) => goTo(String(v))}>
          <TabsList>
            {known.map((slug) => (
              <TabsTrigger key={slug} value={slug} className="px-3 font-mono text-xs">
                {slug}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="New environment"
          onClick={() => setAdding(true)}
        >
          <PlusIcon />
        </Button>

        {env && env !== DEFAULT_ENV && (
          <DeleteEnvironment
            path={path}
            env={env}
            onDeleted={() => {
              goTo(DEFAULT_ENV);
              reloadEnvs();
            }}
            onError={setError}
          />
        )}
      </div>

      {shown?.length === 0 && (
        <Card>
          <CardContent className="flex items-center justify-between gap-4 py-2">
            <span className="text-sm text-muted-foreground">
              Nothing deployed in <code className="text-foreground">{env}</code> yet.
            </span>
            <Button
              variant="outline"
              nativeButton={false}
              render={<Link href={`/apps/new?project=${project}&env=${env}`}>Create an app</Link>}
            />
          </CardContent>
        </Card>
      )}

      {shown && shown.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {shown.map((a) => (
            <AppCard key={a.reference} app={a} />
          ))}
        </div>
      )}

      <NewEnvironmentDialog
        path={path}
        open={adding}
        onOpenChange={setAdding}
        onCreated={(slug) => {
          reloadEnvs();
          goTo(slug);
        }}
      />
    </>
  );
}

function DeleteEnvironment({
  path,
  env,
  onDeleted,
  onError,
}: {
  path: string;
  env: string;
  onDeleted: () => void;
  onError: (m: string) => void;
}) {
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label={`Delete ${env}`}
      className="text-muted-foreground hover:text-destructive"
      onClick={async () => {
        try {
          await api.del(`${path}/environments/${env}`);
          onDeleted();
        } catch (e) {
          onError(message(e));
        }
      }}
    >
      <Trash2Icon />
    </Button>
  );
}

function NewEnvironmentDialog({
  path,
  open,
  onOpenChange,
  onCreated,
}: {
  path: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (slug: string) => void;
}) {
  const [slug, setSlug] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(`${path}/environments`, { slug, name: slug });
      onCreated(slug);
      setSlug("");
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>New environment</DialogTitle>
            <DialogDescription>
              It becomes the third segment of every app reference inside it, so it is a slug and not
              a name.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-5">
            <ErrorAlert error={error} className="mb-0" />
            <TextField
              label="Slug"
              className="font-mono"
              spellCheck={false}
              autoFocus
              value={slug}
              onChange={(e) => setSlug(sanitize(e.target.value))}
              placeholder="staging"
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!slug}>
              Create
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
