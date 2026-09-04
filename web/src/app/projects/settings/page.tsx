"use client";

import { ChevronLeftIcon } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { Shell } from "@/components/shell";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { api, type Project } from "@/lib/api";
import { message } from "@/lib/errors";

export default function ProjectSettingsPage() {
  return (
    <Shell>
      <Suspense>
        <Settings />
      </Suspense>
    </Shell>
  );
}

function Settings() {
  const router = useRouter();
  const params = useSearchParams();
  const ref = params.get("ref") ?? "";
  const [org, project] = ref.split("/");

  const [current, setCurrent] = useState<Project | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const path = `/orgs/${org}/projects/${project}`;

  // There is no "get one project" endpoint — the list is the read, and
  // on a single-VPS install it is a handful of rows.
  useEffect(() => {
    if (!org || !project) return;
    api
      .get<Project[]>(`/orgs/${org}/projects`)
      .then((list) => {
        const found = list.find((p) => p.slug === project);
        if (!found) throw new Error("project not found");
        setCurrent(found);
        setName(found.name);
        setDescription(found.description ?? "");
      })
      .catch((e) => setError(message(e)));
  }, [org, project]);

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

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      // PATCH, so sending both is a statement about both and neither is
      // cleared by having been left off the form.
      setCurrent(await api.patch<Project>(path, { name, description }));
      setSaved(true);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  const dirty = !!current && (name !== current.name || description !== (current.description ?? ""));

  return (
    <>
      <Link
        href={`/projects?ref=${ref}`}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {org}/{project}
      </Link>

      <PageHeader title="Project settings" sub="What this project is called, and what it is for." />

      <ErrorAlert error={error} />

      <SectionHeader title="General" />
      <Card>
        <CardContent>
          <form onSubmit={save} className="space-y-4">
            <TextField
              label="Name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={!current}
            />

            <div className="space-y-2">
              <Label htmlFor="project-description" className="text-xs text-muted-foreground">
                Description
              </Label>
              <textarea
                id="project-description"
                data-slot="input"
                rows={3}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                disabled={!current}
                placeholder="What this project holds, and who it is for."
                className="w-full resize-y border border-input bg-background px-3 py-2 text-sm leading-relaxed outline-none disabled:opacity-50"
              />
              <p className="text-xs text-subtle-foreground">
                Shown on the project's card. Empty is fine.
              </p>
            </div>

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">Slug</Label>
              <div className="flex h-10 items-center border border-border bg-secondary/40 px-3 font-mono text-sm text-muted-foreground">
                {org}/{project}
              </div>
              <p className="text-xs text-subtle-foreground">
                Not editable. It is a path component of every registry reference under this project,
                so changing it would break every push already configured against it.
              </p>
            </div>

            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={busy} disabled={!dirty || !name}>
                Save
              </ActionButton>
              {saved && !dirty && <span className="text-xs text-muted-foreground">Saved.</span>}
            </div>
          </form>
        </CardContent>
      </Card>

      <DangerZone>
        <DangerAction
          title="Delete this project"
          description={
            <>
              Removes the project and every environment in it. Refused while any app still lives
              here — delete those first, since removing an app means stopping its container.
            </>
          }
          action={
            <Button variant="destructive" onClick={() => setDeleting(true)}>
              Delete project
            </Button>
          }
        />
      </DangerZone>

      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title="Delete project"
        description="Every environment in it goes too. This cannot be undone."
        confirmWord={project}
        confirmLabel="Delete project"
        onConfirm={async () => {
          await api.del(path);
          router.push("/");
        }}
      />
    </>
  );
}
