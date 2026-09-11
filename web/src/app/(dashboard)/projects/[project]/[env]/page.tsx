"use client";

import { useQuery } from "@tanstack/react-query";
import { PlusIcon, SettingsIcon, SlidersHorizontalIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { startTransition, use, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AppCard } from "@/components/app-card";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal, RailTabs } from "@/components/header-rail";
import { SlugField } from "@/components/slug-field";
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

  const [adding, setAdding] = useState(false);
  const [creatingApp, setCreatingApp] = useState(false);

  const path = `/projects/${project}`;

  // **Through the query cache, and that is what stops the blink.**
  //
  // The environment is a path segment, so switching one is a navigation
  // — and this page reads its segments with `use(params)`, a promise
  // that is new every time, which suspends and takes the component
  // down with it. On the way back up a hand-rolled `useState(null)` is
  // null again and the fetch starts over, so the grid emptied for as
  // long as the round trip took: a blank frame between two lists that
  // are mostly the same apps.
  //
  // A cached query comes back holding what it had. Nothing needed
  // refetching anyway — `/apps` is every app on the instance, and the
  // environment only decides which of them are drawn.
  const envs = useQuery({
    queryKey: ["environments", project],
    queryFn: () => api.get<Environment[]>(`${path}/environments`),
    enabled: Boolean(project),
  });
  const apps = useQuery({ queryKey: ["apps"], queryFn: () => api.get<App[]>("/apps") });
  const error = envs.error ?? apps.error;

  if (!project) {
    return (
      <p className="text-sm text-muted-foreground">
        No project named.{" "}
        <Link href="/projects" className="text-foreground underline underline-offset-4">
          Back to projects
        </Link>
        .
      </p>
    );
  }

  const known = envs.data?.map((e) => e.slug) ?? [];
  const env = known.includes(wanted)
    ? wanted
    : known.includes(DEFAULT_ENV)
      ? DEFAULT_ENV
      : (known[0] ?? "");

  const shown = apps.data?.filter((a) => a.project === project && a.environment === env);

  // In a transition, so React keeps the screen it has while the new
  // route resolves rather than swapping in a suspense fallback. The
  // cache above is what makes there be something to keep.
  function goTo(next: string) {
    startTransition(() => {
      router.replace(`/projects/${project}/${next}`, { scroll: false });
    });
  }

  return (
    <>
      <RailPortal>
        {
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
      </RailPortal>
      <ErrorAlert error={error ? message(error) : null} />

      {/* **The environments are the rail's tabs, not the page's.**
          They are a level of the same hierarchy the crumbs above them
          spell out, and switching one changes the address — which is
          what a tab is here and what a row of buttons in the page never
          quite read as. They were a filled switcher inside the content,
          which put the thing that says *where you are* below the thing
          that says what you are looking at.

          The `+` sits at the end of them, where a browser puts it. */}
      <RailTabs>
        <Tabs value={env} onValueChange={(v) => goTo(String(v))} className="contents">
          <TabsList variant="line">
            {known.map((slug) => (
              <TabsTrigger key={slug} value={slug}>
                {slug}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <button
          type="button"
          aria-label="New environment"
          title="New environment"
          onClick={() => setAdding(true)}
          className="flex shrink-0 items-center px-3 text-subtle-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <PlusIcon className="size-3.5" />
        </button>

        {env && (
          <Link
            href={`/projects/${project}/${env}/settings`}
            aria-label={`Settings for ${env}`}
            title={`Settings for ${env}`}
            className="ml-auto flex shrink-0 items-center px-3 text-subtle-foreground transition-colors hover:bg-secondary hover:text-foreground"
          >
            <SlidersHorizontalIcon className="size-3.5" />
          </Link>
        )}
      </RailTabs>

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
          envs.refetch();
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
            <ErrorAlert error={error ? message(error) : null} />
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

// An app is created with a slug and nothing else. What it runs and
// where it is served are decisions with consequences —
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
      });
      setSlug("");
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
            <ErrorAlert error={error ? message(error) : null} />
            <SlugField autoFocus value={slug} onChange={setSlug} placeholder="gateway" />
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
