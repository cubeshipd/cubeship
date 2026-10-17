"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  FileTextIcon,
  FolderTreeIcon,
  PauseIcon,
  PlayIcon,
  PlusIcon,
  RocketIcon,
  TerminalSquareIcon,
  Trash2Icon,
} from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { createContext, type ReactNode, useContext, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import Link from "@/components/navigation-link";
import { NewWorkloadAppDialog } from "@/components/new-workload-app-dialog";
import { SearchBar } from "@/components/search-bar";
import { useSession } from "@/components/session-context";
import { MobileNavigation } from "@/components/shell";
import { SlugField } from "@/components/slug-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, can, type Environment } from "@/lib/api";
import { projectsQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

type AppWorkspaceActions = {
  deploy?: () => Promise<void>;
  deleteApp?: () => void;
  logs?: () => void;
  shell?: () => void;
  stop?: () => void;
  start?: () => Promise<void>;
  deploying?: boolean;
  starting?: boolean;
  canStop?: boolean;
  canStart?: boolean;
  deployError?: string | null;
  actionError?: string | null;
};

const AppWorkspaceActionsContext = createContext<{
  actions: AppWorkspaceActions;
  setActions: (actions: AppWorkspaceActions) => void;
} | null>(null);

export function useAppWorkspaceActions() {
  const value = useContext(AppWorkspaceActionsContext);
  if (!value) throw new Error("useAppWorkspaceActions must be used inside ProjectWorkspace");
  return value;
}

export function ProjectWorkspace({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const me = useSession();
  const queryClient = useQueryClient();
  const [, , selected, routeEnvironment, routeApp] = pathname.split("/");
  const insideApp = Boolean(routeApp && routeApp !== "settings");
  const projects = useQuery(projectsQuery);
  const environments = useQuery({
    queryKey: ["environments", selected],
    queryFn: () => api.get<Environment[]>(`/projects/${selected}/environments`),
    enabled: Boolean(selected),
  });
  const [creatingApp, setCreatingApp] = useState(false);
  const [addingEnvironment, setAddingEnvironment] = useState(false);
  const [deletingEnvironment, setDeletingEnvironment] = useState(false);
  const [deletingProject, setDeletingProject] = useState(false);
  const [projectSearch, setProjectSearch] = useState("");
  const [appActions, setAppActions] = useState<AppWorkspaceActions>({});
  const canCreateApp = can(me, "apps", "manage");
  const canManageProjects = can(me, "projects", "manage");
  const emptyWorkspace = pathname === "/projects";
  const filteredProjects = useMemo(() => {
    const query = projectSearch.trim().toLowerCase();
    return (projects.data ?? []).filter((project) => project.slug.toLowerCase().includes(query));
  }, [projectSearch, projects.data]);

  return (
    <div
      className={
        emptyWorkspace
          ? "flex min-h-[calc(100dvh-156px)] min-w-0 flex-col lg:min-h-[calc(100dvh-100px)]"
          : "min-w-0"
      }
    >
      <div className="mb-3 flex justify-end lg:hidden">
        <MobileNavigation me={me} />
      </div>
      <div className="grid min-w-0 flex-1 gap-5 lg:grid-cols-[13rem_minmax(0,1fr)]">
        <aside className="min-w-0 lg:sticky lg:top-4 lg:self-start">
          <section className="mb-5">
            <h2 className="mb-2 px-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
              Actions
            </h2>
            <div className="grid gap-1">
              {insideApp ? (
                <>
                  <Button
                    variant="ghost"
                    className="w-full justify-start"
                    disabled={!appActions.deploy || appActions.deploying}
                    onClick={() => void appActions.deploy?.()}
                  >
                    <RocketIcon />
                    Deploy
                  </Button>
                  {appActions.deployError && (
                    <p role="alert" className="px-3 text-xs text-destructive">
                      {appActions.deployError}
                    </p>
                  )}
                  {appActions.canStop && (
                    <Button
                      variant="ghost"
                      className="w-full justify-start"
                      onClick={appActions.stop}
                    >
                      <PauseIcon />
                      Stop
                    </Button>
                  )}
                  {appActions.canStart && (
                    <Button
                      variant="ghost"
                      className="w-full justify-start"
                      disabled={appActions.starting}
                      onClick={() => void appActions.start?.()}
                    >
                      <PlayIcon />
                      {appActions.starting ? "Starting…" : "Start"}
                    </Button>
                  )}
                  {appActions.actionError && (
                    <p role="alert" className="px-3 text-xs text-destructive">
                      {appActions.actionError}
                    </p>
                  )}
                  {appActions.logs && (
                    <Button
                      variant="ghost"
                      className="w-full justify-start"
                      onClick={appActions.logs}
                    >
                      <FileTextIcon />
                      Logs
                    </Button>
                  )}
                  {appActions.shell && (
                    <Button
                      variant="ghost"
                      className="w-full justify-start"
                      onClick={appActions.shell}
                    >
                      <TerminalSquareIcon />
                      Shell
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    className="w-full justify-start text-destructive transition-colors hover:bg-destructive/20 dark:hover:bg-destructive/20 hover:text-destructive"
                    disabled={!appActions.deleteApp}
                    onClick={() => appActions.deleteApp?.()}
                  >
                    <Trash2Icon />
                    Delete app
                  </Button>
                </>
              ) : (
                <>
                  {canCreateApp && (
                    <Button
                      variant="ghost"
                      className="w-full justify-start"
                      onClick={() => setCreatingApp(true)}
                    >
                      <PlusIcon />
                      New app
                    </Button>
                  )}
                  {selected && canManageProjects && (
                    <Button
                      variant="ghost"
                      className="w-full justify-start"
                      onClick={() => setAddingEnvironment(true)}
                    >
                      <PlusIcon />
                      New environment
                    </Button>
                  )}
                  {selected &&
                    canManageProjects &&
                    routeEnvironment &&
                    routeEnvironment !== "settings" && (
                      <>
                        <Button
                          variant="ghost"
                          className="w-full justify-start text-destructive transition-colors hover:bg-destructive/20 dark:hover:bg-destructive/20 hover:text-destructive"
                          disabled={routeEnvironment === "production"}
                          title={
                            routeEnvironment === "production"
                              ? "The production environment cannot be deleted"
                              : undefined
                          }
                          onClick={() => setDeletingEnvironment(true)}
                        >
                          <Trash2Icon />
                          Delete environment
                        </Button>
                        <Button
                          variant="ghost"
                          className="w-full justify-start text-destructive transition-colors hover:bg-destructive/20 dark:hover:bg-destructive/20 hover:text-destructive"
                          onClick={() => setDeletingProject(true)}
                        >
                          <Trash2Icon />
                          Delete project
                        </Button>
                      </>
                    )}
                </>
              )}
            </div>
          </section>
          <h2 className="mb-2 px-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Projects
          </h2>
          {(projects.data?.length ?? 0) > 0 && (
            <SearchBar
              value={projectSearch}
              onChange={setProjectSearch}
              placeholder="Filter projects"
              className="mb-2"
            />
          )}
          <nav
            aria-label="Projects"
            className="flex gap-1 overflow-x-auto rounded-xl border border-border bg-card p-1 lg:flex-col lg:overflow-visible lg:border-0 lg:bg-transparent lg:p-0"
          >
            {projects.isPending && (
              <p role="status" className="px-3 py-2 text-xs text-muted-foreground">
                Loading projects…
              </p>
            )}
            {filteredProjects.map((project) => {
              const active = selected === project.slug;
              return (
                <Link
                  key={project.slug}
                  href={`/projects/${project.slug}/production`}
                  aria-current={active ? "page" : undefined}
                  className={`flex min-h-10 shrink-0 items-center gap-2 rounded-lg px-3 text-sm transition-colors lg:w-full ${active ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-secondary hover:text-foreground"}`}
                >
                  <FolderTreeIcon className="size-4 shrink-0" />
                  <span className="truncate">{project.slug}</span>
                </Link>
              );
            })}
            {projects.data && projects.data.length > 0 && filteredProjects.length === 0 && (
              <p className="px-3 py-2 text-xs text-muted-foreground">No projects match.</p>
            )}
            {projects.isError && (
              <p className="px-3 py-2 text-xs text-muted-foreground">Couldn’t load projects.</p>
            )}
            {projects.data?.length === 0 && (
              <p className="px-3 py-2 text-xs text-muted-foreground">No projects yet.</p>
            )}
          </nav>
        </aside>
        <AppWorkspaceActionsContext.Provider
          value={{ actions: appActions, setActions: setAppActions }}
        >
          <div className={`min-w-0 ${emptyWorkspace ? "flex flex-col" : ""}`}>{children}</div>
        </AppWorkspaceActionsContext.Provider>
        <NewWorkloadAppDialog
          open={creatingApp}
          onOpenChange={setCreatingApp}
          initialProject={selected}
          initialEnvironment={routeEnvironment !== "settings" ? routeEnvironment : undefined}
          onCreated={(reference) => {
            setCreatingApp(false);
            router.push(`/projects/${reference}`);
          }}
        />
        <NewEnvironmentDialog
          project={selected ?? ""}
          open={addingEnvironment}
          onOpenChange={setAddingEnvironment}
          onCreated={(slug) => {
            void environments.refetch();
            router.push(`/projects/${selected}/${slug}`);
          }}
        />
        <ConfirmDialog
          open={deletingEnvironment}
          onOpenChange={setDeletingEnvironment}
          title={`Delete environment ${routeEnvironment}?`}
          description="The environment, the variables set on it and every app in it go — containers included. This cannot be undone."
          holdToConfirm
          confirmLabel="Delete environment"
          onConfirm={async () => {
            await api.del(`/projects/${selected}/environments/${routeEnvironment}`);
            await queryClient.invalidateQueries({ queryKey: ["apps"] });
            await environments.refetch();
            router.push(`/projects/${selected}/production`);
          }}
        />
        <ConfirmDialog
          open={deletingProject}
          onOpenChange={setDeletingProject}
          title={`Delete project ${selected}?`}
          description="Every environment in it and every app inside those go — containers included. This cannot be undone."
          holdToConfirm
          confirmLabel="Delete project"
          onConfirm={async () => {
            await api.del(`/projects/${selected}`);
            await queryClient.invalidateQueries({ queryKey: ["apps"] });
            await projects.refetch();
            router.push("/projects");
          }}
        />
      </div>
    </div>
  );
}

function NewEnvironmentDialog({
  project,
  open,
  onOpenChange,
  onCreated,
}: {
  project: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (slug: string) => void;
}) {
  const [slug, setSlug] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(`/projects/${project}/environments`, { slug });
      onCreated(slug);
      setSlug("");
      onOpenChange(false);
    } catch (cause) {
      setError(message(cause));
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
              Create environment
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
