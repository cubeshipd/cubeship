"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { LayersIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
import { use, useState, useTransition } from "react";
import { AppCard } from "@/components/app-card";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import Link, { NavigationProgress } from "@/components/navigation-link";
import { NewEnvironmentDialog } from "@/components/new-environment-dialog";
import { NewWorkloadAppDialog } from "@/components/new-workload-app-dialog";
import { PageLoading } from "@/components/page-loading";
import { type MenuItem, ResourceHeader } from "@/components/resource-header";
import { useSession } from "@/components/session-context";
import { Button, buttonVariants } from "@/components/ui/button";
import { type App, api, can, type Environment } from "@/lib/api";
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
const ADD_ENV = "\u0000new-environment";

function Detail({ project, env: wanted }: { project: string; env: string }) {
  const router = useRouter();
  const me = useSession();
  const queryClient = useQueryClient();
  const [navigating, startTransition] = useTransition();

  const [creatingApp, setCreatingApp] = useState(false);
  const [addingEnvironment, setAddingEnvironment] = useState(false);
  const [deletingEnvironment, setDeletingEnvironment] = useState(false);
  const [deletingProject, setDeletingProject] = useState(false);

  const path = `/projects/${project}`;
  const canManageProjects = can(me, "projects", "manage");

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

  const menu: MenuItem[] = [
    ...(canManageProjects && env && env !== DEFAULT_ENV
      ? [
          {
            id: "delete-environment",
            label: "Delete environment",
            icon: <Trash2Icon className="size-4" />,
            destructive: true,
            onSelect: () => setDeletingEnvironment(true),
          },
        ]
      : []),
    ...(canManageProjects
      ? [
          {
            id: "delete-project",
            label: "Delete project",
            icon: <Trash2Icon className="size-4" />,
            destructive: true,
            onSelect: () => setDeletingProject(true),
          },
        ]
      : []),
  ];

  return (
    <>
      <NavigationProgress pending={navigating} />
      <ResourceHeader
        crumbs={[{ label: "Projects", href: "/projects" }]}
        title={project}
        menu={menu}
        menuLabel={`Actions for ${project}`}
        actions={
          <>
            <GlideSelect
              ariaLabel="Environment"
              value={env}
              options={[
                ...known.map((slug) => ({ value: slug, label: slug })),
                ...(canManageProjects
                  ? [
                      {
                        value: ADD_ENV,
                        label: (
                          <span className="inline-flex items-center gap-2">
                            <PlusIcon className="size-4" />
                            Add environment
                          </span>
                        ),
                        searchText: "add environment new",
                      },
                    ]
                  : []),
              ]}
              onChange={(value) => (value === ADD_ENV ? setAddingEnvironment(true) : goTo(value))}
              searchable
              startIcon={<LayersIcon className="size-4" />}
              align="right"
              busy={envs.isPending}
              disabled={known.length === 0}
              triggerClassName={`${buttonVariants({ variant: "outline", size: "lg" })} min-w-40 justify-between`}
            />
            {can(me, "apps", "manage") && (
              <Button size="lg" onClick={() => setCreatingApp(true)}>
                <PlusIcon />
                New app
              </Button>
            )}
          </>
        }
      />
      <ErrorAlert error={error ? message(error) : null} />

      {apps.isPending && <PageLoading kind="grid" label="Loading apps" />}

      {shown?.length === 0 && (
        <div className="flex items-center justify-between gap-4 py-10">
          <span className="text-sm text-muted-foreground">
            Nothing deployed in <code className="text-foreground">{env}</code> yet.
          </span>
          {can(me, "apps", "manage") && (
            <Button variant="outline" onClick={() => setCreatingApp(true)}>
              Create an app
            </Button>
          )}
        </div>
      )}

      {shown && shown.length > 0 && (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
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
        onCreated={(reference) => {
          setCreatingApp(false);
          router.push(`/projects/${reference}`);
        }}
      />
      <NewEnvironmentDialog
        project={project}
        open={addingEnvironment}
        onOpenChange={setAddingEnvironment}
        onCreated={(slug) => {
          void queryClient.invalidateQueries({ queryKey: ["environments", project] });
          void queryClient.invalidateQueries({ queryKey: ["projects"] });
          router.push(`/projects/${project}/${slug}`);
        }}
      />
      <ConfirmDialog
        open={deletingEnvironment}
        onOpenChange={setDeletingEnvironment}
        title={`Delete environment ${env}?`}
        description="The environment, the variables set on it and every app in it go — containers included. This cannot be undone."
        holdToConfirm
        confirmLabel="Delete environment"
        onConfirm={async () => {
          await api.del(`${path}/environments/${env}`);
          await queryClient.invalidateQueries({ queryKey: ["apps"] });
          await queryClient.invalidateQueries({ queryKey: ["projects"] });
          await envs.refetch();
          router.push(`${path}/${DEFAULT_ENV}`);
        }}
      />
      <ConfirmDialog
        open={deletingProject}
        onOpenChange={setDeletingProject}
        title={`Delete project ${project}?`}
        description="Every environment in it and every app inside those go — containers included. This cannot be undone."
        holdToConfirm
        confirmLabel="Delete project"
        onConfirm={async () => {
          await api.del(path);
          await queryClient.invalidateQueries({ queryKey: ["apps"] });
          await queryClient.invalidateQueries({ queryKey: ["projects"] });
          router.push("/projects");
        }}
      />
    </>
  );
}
