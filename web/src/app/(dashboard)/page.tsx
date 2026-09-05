"use client";

import { PlusIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { useOrg } from "@/components/org-context";
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

// The projects in the selected organization, as cards. Apps are not
// listed here: an app only means something inside an environment, and
// which environment is a choice you make after opening the project.
function Projects() {
  const { org, loaded } = useOrg();
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [envs, setEnvs] = useState<Record<string, string[]>>({});
  const [apps, setApps] = useState<App[]>([]);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    if (!org) {
      setProjects(null);
      return;
    }
    api
      .get<Project[]>(`/orgs/${org}/projects`)
      .then(setProjects)
      .catch((e) => setError(message(e)));
  }, [org]);
  useEffect(reload, [reload]);

  // The daemon answers with every app you can see, across organizations
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
    if (!org || !projects) return;
    let live = true;
    Promise.all(
      projects.map((p) =>
        api
          .get<Environment[]>(`/orgs/${org}/projects/${p.slug}/environments`)
          .then((list) => [p.slug, list.map((e) => e.slug)] as const)
          .catch(() => [p.slug, []] as const),
      ),
    ).then((pairs) => live && setEnvs(Object.fromEntries(pairs)));
    return () => {
      live = false;
    };
  }, [org, projects]);

  return (
    <>
      <PageHeader
        title="Projects"
        sub={
          org ? (
            <>
              Everything under <code className="text-foreground">{org}</code>. An app lives in an
              environment, inside one of these.
            </>
          ) : (
            "An app lives in an environment, inside a project, inside an organization."
          )
        }
        actions={
          org && (
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              New project
            </Button>
          )
        }
      />

      <ErrorAlert error={error} />

      {loaded && !org && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            No organization selected. Create one from the switcher at the top of the sidebar.
          </CardContent>
        </Card>
      )}

      {projects?.length === 0 && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            No projects in this organization yet.
          </CardContent>
        </Card>
      )}

      {projects && projects.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {projects.map((p) => (
            <ProjectCard
              key={p.slug}
              org={org}
              slug={p.slug}
              description={p.description}
              environments={envs[p.slug] ?? p.environments ?? []}
              apps={apps.filter((a) => a.org === org && a.project === p.slug)}
            />
          ))}
        </div>
      )}

      {org && (
        <NewProjectDialog org={org} open={creating} onOpenChange={setCreating} onCreated={reload} />
      )}
    </>
  );
}

function NewProjectDialog({
  org,
  open,
  onOpenChange,
  onCreated,
}: {
  org: string;
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
      await api.post(`/orgs/${org}/projects`, { slug });
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
            <ErrorAlert error={error} className="mb-0" />
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
