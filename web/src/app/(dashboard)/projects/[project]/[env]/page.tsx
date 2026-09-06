"use client";

import { PlusIcon, SettingsIcon, SlidersHorizontalIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AppCard } from "@/components/app-card";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader } from "@/components/page-header";
import { SlugField } from "@/components/slug-field";
import { TextAreaField } from "@/components/text-field";
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

// One project, opened on an environment.
//
// The environment is in the path rather than in a tab's state: it is
// what identifies the apps below it, so a link someone sends opens the
// same screen they were looking at.
export default function ProjectPage({ params }: PageProps<"/projects/[project]/[env]">) {
  return <Detail {...use(params)} />;
}

// production is the environment a project always has and cannot lose,
// so it is where the project opens.
const DEFAULT_ENV = "production";

function Detail({ project, env: wanted }: { project: string; env: string }) {
  const router = useRouter();

  const [envs, setEnvs] = useState<Environment[] | null>(null);
  const [apps, setApps] = useState<App[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [creatingApp, setCreatingApp] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const path = `/projects/${project}`;
  const reloadEnvs = useCallback(() => {
    if (!project) return;
    api
      .get<Environment[]>(`${path}/environments`)
      .then(setEnvs)
      .catch((e) => setError(message(e)));
  }, [path, project]);
  useEffect(reloadEnvs, [reloadEnvs]);

  const reloadApps = useCallback(() => {
    api
      .get<App[]>("/apps")
      .then(setApps)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reloadApps, [reloadApps]);

  if (!project) {
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

  const shown = apps?.filter((a) => a.project === project && a.environment === env);

  function goTo(next: string) {
    router.replace(`/projects/${project}/${next}`, { scroll: false });
  }

  return (
    <>
      <PageHeader
        title={project}
        actions={
          <>
            <Button onClick={() => setCreatingApp(true)}>
              <PlusIcon />
              New app
            </Button>
            <Button
              variant="outline"
              nativeButton={false}
              render={
                <Link href={`/projects/${project}/settings`}>
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

        {env && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Settings for ${env}`}
            nativeButton={false}
            render={
              <Link href={`/projects/${project}/${env}/settings`}>
                <SlidersHorizontalIcon />
              </Link>
            }
          />
        )}
      </div>

      {shown?.length === 0 && (
        <Card>
          <CardContent className="flex items-center justify-between gap-4 py-2">
            <span className="text-sm text-muted-foreground">
              Nothing deployed in <code className="text-foreground">{env}</code> yet.
            </span>
            <Button variant="outline" onClick={() => setCreatingApp(true)}>
              Create an app
            </Button>
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

      <NewAppDialog
        project={project}
        environment={env}
        open={creatingApp}
        onOpenChange={setCreatingApp}
        onCreated={(reference) => router.push(`/projects/${reference}`)}
      />

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
      await api.post(`${path}/environments`, { slug });
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
            <ErrorAlert error={error} />
            <SlugField autoFocus value={slug} onChange={setSlug} placeholder="staging" />
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

// An app is created with a slug and a description, and nothing else.
// What it runs and where it is served are decisions with consequences —
// a build executes a repository on this host; a domain has to resolve
// here — so they are made inside the app, with the reasons in front of
// you, rather than guessed at in the moment you name it.
function NewAppDialog({
  project,
  environment,
  open,
  onOpenChange,
  onCreated,
}: {
  project: string;
  environment: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (reference: string) => void;
}) {
  const [slug, setSlug] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.post<App>("/apps", {
        project,
        environment,
        name: slug,
        description,
      });
      setSlug("");
      setDescription("");
      onOpenChange(false);
      onCreated(created.reference);
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
            <DialogTitle>New app</DialogTitle>
            <DialogDescription>
              It is created with nothing configured. Set where it is served and where its image
              comes from inside the app, and then deploy it.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <SlugField autoFocus value={slug} onChange={setSlug} placeholder="gateway" />
            <TextAreaField
              label="Description"
              hint="What this app is. Empty is fine."
              rows={3}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
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
