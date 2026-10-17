"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { PlusIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import { useSession } from "@/components/session-context";
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
import { type App, api, type Environment, type Project } from "@/lib/api";
import { projectsQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

const NEW_PROJECT = "__new__";

export function NewWorkloadAppDialog({
  open,
  onOpenChange,
  onCreated,
  initialProject,
  initialEnvironment,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (reference: string) => void;
  initialProject?: string;
  initialEnvironment?: string;
}) {
  const me = useSession();
  const queryClient = useQueryClient();
  const canCreateProject =
    me.grants === null ||
    me.grants.some(
      (grant) => grant.resource === "projects" && grant.level === "manage" && grant.items === null,
    );
  const projects = useQuery({ ...projectsQuery, enabled: open });
  const [project, setProject] = useState("");
  const [newProject, setNewProject] = useState("");
  const [environment, setEnvironment] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const environments = useQuery({
    queryKey: ["environments", project],
    queryFn: () => api.get<Environment[]>(`/projects/${encodeURIComponent(project)}/environments`),
    enabled: open && Boolean(project) && project !== NEW_PROJECT,
  });
  const environmentChoices = (environments.data ?? []).map((item) => ({
    value: item.slug,
    label: item.slug,
  }));
  const selectTriggerClass =
    "flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-md border px-3 text-left text-sm text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 disabled:cursor-not-allowed disabled:opacity-50";

  useEffect(() => {
    if (!open) return;
    setProject(initialProject ?? "");
    setNewProject("");
    setEnvironment(initialEnvironment ?? "");
    setName("");
    setError(null);
  }, [initialEnvironment, initialProject, open]);

  useEffect(() => {
    if (open && canCreateProject && projects.data?.length === 0) setProject(NEW_PROJECT);
  }, [canCreateProject, open, projects.data]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    let createdProject: Project | null = null;
    try {
      let targetProject = project;
      let targetEnvironment = environment;
      if (project === NEW_PROJECT) {
        createdProject = await api.post<Project>("/projects", { slug: newProject.trim() });
        targetProject = createdProject.slug;
        targetEnvironment = createdProject.environments?.[0] ?? "production";
        setProject(targetProject);
        setEnvironment(targetEnvironment);
        const projectToCache = createdProject;
        queryClient.setQueryData<Project[]>(projectsQuery.queryKey, (current) => [
          ...(current ?? []).filter((item) => item.slug !== projectToCache.slug),
          projectToCache,
        ]);
      }
      const created = await api.post<App>("/apps", {
        project: targetProject,
        environment: targetEnvironment,
        name,
      });
      onOpenChange(false);
      onCreated(created.reference);
    } catch (cause) {
      setError(
        createdProject
          ? `Project created, but the app could not be created: ${message(cause)}`
          : message(cause),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>New app</DialogTitle>
            <DialogDescription>
              Choose its project and environment. Configure its image and deploy after creation.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-5">
            <ErrorAlert
              error={
                error ??
                (projects.error
                  ? message(projects.error)
                  : environments.error
                    ? message(environments.error)
                    : null)
              }
            />
            <GlideSelect
              label="Project"
              value={project}
              onChange={(value) => {
                setProject(value);
                setEnvironment("");
              }}
              busy={projects.isLoading}
              options={[
                ...(canCreateProject
                  ? [
                      {
                        value: NEW_PROJECT,
                        label: (
                          <span className="inline-flex items-center gap-2">
                            <PlusIcon className="size-4" aria-hidden="true" />
                            New project
                          </span>
                        ),
                        tag: "Created with this app",
                        searchText: "New project",
                      },
                    ]
                  : []),
                ...(projects.data ?? []).map((item) => ({
                  value: item.slug,
                  label: item.slug,
                })),
              ]}
              searchable
              emptyMessage="No projects found."
              className="w-full"
              triggerClassName={selectTriggerClass}
              placeholder="Select project"
            />
            {project === NEW_PROJECT ? (
              <>
                <SlugField label="Project name" value={newProject} onChange={setNewProject} />
                <p className="text-xs text-muted-foreground">
                  New projects start with a production environment.
                </p>
              </>
            ) : (
              <GlideSelect
                label="Environment"
                value={environment}
                onChange={setEnvironment}
                options={environmentChoices}
                searchable
                placeholder="Select environment"
                disabled={!project || environments.isPending}
                busy={Boolean(project) && environments.isPending}
                emptyMessage="No environments found."
                className="w-full"
                triggerClassName={selectTriggerClass}
              />
            )}
            <SlugField label="Name" value={name} onChange={setName} autoFocus />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton
              type="submit"
              busy={busy}
              disabled={!project || !name || (project === NEW_PROJECT ? !newProject : !environment)}
            >
              Create
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
