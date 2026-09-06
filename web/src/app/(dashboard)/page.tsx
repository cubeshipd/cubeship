"use client";

import { PlusIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader } from "@/components/page-header";
import { ProjectCard } from "@/components/project-card";
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
import { type App, api, type Environment, type Project } from "@/lib/api";
import { message } from "@/lib/errors";

// An unclaimed instance is not this page's problem: the shell above
// sends anyone it cannot identify to sign in, and sign-in is where an
// instance with no account at all redirects to setup. Checking here as
// well only added a blank frame to every visit.
export default function Home() {
  return <Projects />;
}

// The instance's projects, as cards. Apps are not
// listed here: an app only means something inside an environment, and
// which environment is a choice you make after opening the project.
function Projects() {
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [envs, setEnvs] = useState<Record<string, string[]>>({});
  const [apps, setApps] = useState<App[]>([]);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api
      .get<Project[]>(`/projects`)
      .then(setProjects)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  // The daemon answers with every app you can see, across every project
  // — one request, and the cards count out of it.
  useEffect(() => {
    api
      .get<App[]>("/apps")
      .then(setApps)
      .catch(() => setApps([]));
  }, []);

  // Environments come one project at a time; a card that showed none
  // until you opened it would be the wrong shape of empty.
  useEffect(() => {
    if (!projects) return;
    let live = true;
    Promise.all(
      projects.map((p) =>
        api
          .get<Environment[]>(`/projects/${p.slug}/environments`)
          .then((list) => [p.slug, list.map((e) => e.slug)] as const)
          .catch(() => [p.slug, []] as const),
      ),
    ).then((pairs) => live && setEnvs(Object.fromEntries(pairs)));
    return () => {
      live = false;
    };
  }, [projects]);

  return (
    <>
      <PageHeader
        title="Projects"
        sub="An app lives in an environment, inside a project."
        actions={
          <Button onClick={() => setCreating(true)}>
            <PlusIcon />
            New project
          </Button>
        }
      />

      <ErrorAlert error={error} />

      {projects?.length === 0 && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            No projects yet. A project holds your environments, and each environment holds the apps
            you deploy — start with{" "}
            <button
              type="button"
              onClick={() => setCreating(true)}
              className="text-foreground underline underline-offset-4"
            >
              New project
            </button>
            .
          </CardContent>
        </Card>
      )}

      {projects && projects.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {projects.map((p) => (
            <ProjectCard
              key={p.slug}
              slug={p.slug}
              description={p.description}
              environments={envs[p.slug] ?? p.environments ?? []}
              apps={apps.filter((a) => a.project === p.slug)}
            />
          ))}
        </div>
      )}

      <NewProjectDialog open={creating} onOpenChange={setCreating} onCreated={reload} />
    </>
  );
}

function NewProjectDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: () => void;
}) {
  const [slug, setSlug] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      // No name: the daemon derives one from the slug, and it is edited
      // afterwards in settings if the guess is wrong.
      await api.post(`/projects`, { slug });
      setSlug("");
      onCreated();
      onOpenChange(false);
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
            <DialogTitle>New project</DialogTitle>
            <DialogDescription>
              It starts with a <code>production</code> environment. Others are added from inside the
              project.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            <SlugField autoFocus value={slug} onChange={setSlug} placeholder="public-api" />
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
