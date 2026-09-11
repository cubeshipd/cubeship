"use client";

import { PlusIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal } from "@/components/header-rail";
import { ProjectCard } from "@/components/project-card";
import { SearchBar } from "@/components/search-bar";
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
import { type App, api, type Project } from "@/lib/api";
import { message } from "@/lib/errors";
import { useOpenOnArrival } from "@/lib/open-on-arrival";

// An unclaimed instance is not this page's problem: the shell above
// sends anyone it cannot identify to sign in, and sign-in is where an
// instance with no account at all redirects to setup. Checking here as
// well only added a blank frame to every visit.
//
// It was `/` for as long as there was nothing else to land on. What
// took that address is the Overview, and this moved to the address it
// was always the index of: an app is `/projects/<project>/<env>/<app>`,
// so the list of projects is `/projects`.
export default function ProjectsPage() {
  return <Projects />;
}

// The instance's projects, as cards. Apps are not
// listed here: an app only means something inside an environment, and
// which environment is a choice you make after opening the project.
function Projects() {
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [apps, setApps] = useState<App[]>([]);
  const [creating, setCreating] = useState(false);
  useOpenOnArrival("new", setCreating);
  const [query, setQuery] = useState("");
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

  const needle = query.trim().toLowerCase();
  const shown = (projects ?? []).filter((p) => p.slug.toLowerCase().includes(needle));

  return (
    <>
      <RailPortal>
        {
          <Button onClick={() => setCreating(true)}>
            <PlusIcon />
            New project
          </Button>
        }
      </RailPortal>
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

      {/* The grid is not a DataTable, so the filter is written out
          here — the same control, the same gap, and the same rule about
          what it filters: the page, not one card. */}
      {projects && projects.length > 0 && (
        <SearchBar
          className="mb-4"
          value={query}
          onChange={setQuery}
          placeholder="Filter projects"
          trailing={
            <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
              {shown.length}/{projects.length}
            </span>
          }
        />
      )}

      {projects && projects.length > 0 && shown.length === 0 && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            Nothing matches that.
          </CardContent>
        </Card>
      )}

      {shown.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {shown.map((p) => (
            <ProjectCard
              key={p.slug}
              slug={p.slug}
              hasImage={p.has_image}
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
