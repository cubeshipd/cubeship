"use client";

import { useQuery } from "@tanstack/react-query";
import { LayersIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { use, useState, useTransition } from "react";
import { AppCard } from "@/components/app-card";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import Link, { NavigationProgress } from "@/components/navigation-link";
import { NewWorkloadAppDialog } from "@/components/new-workload-app-dialog";
import { PageLoading } from "@/components/page-loading";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { type App, api, type Environment } from "@/lib/api";
import { message } from "@/lib/errors";
import { usageShares, useContainerUsage } from "@/lib/usage";

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
  const [navigating, startTransition] = useTransition();

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
  const { usage, machine } = useContainerUsage("app");
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
      <NavigationProgress pending={navigating} />
      <ErrorAlert error={error ? message(error) : null} />

      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-wrap items-end gap-2">
          <GlideSelect
            label="Environment"
            value={env}
            options={known.map((slug) => ({ value: slug, label: slug }))}
            onChange={(value) => goTo(value)}
            searchable
            startIcon={<LayersIcon className="size-4" />}
            className="w-52"
            busy={envs.isPending}
            disabled={known.length === 0}
            triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 disabled:cursor-not-allowed disabled:opacity-50"
          />
        </div>
      </div>

      {apps.isPending && <PageLoading kind="grid" label="Loading apps" />}

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
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {shown.map((a) => (
            <AppCard
              key={a.reference}
              app={a}
              shares={usageShares(usage?.get(a.reference), a.limits, machine)}
            />
          ))}
        </div>
      )}

      <NewWorkloadAppDialog
        open={creatingApp}
        onOpenChange={setCreatingApp}
        initialProject={project}
        initialEnvironment={env}
        onCreated={(reference) => router.push(`/projects/${reference}`)}
      />
    </>
  );
}
