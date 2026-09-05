"use client";

import { ChevronLeftIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { TextAreaField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { api, type Project } from "@/lib/api";
import { message } from "@/lib/errors";

export default function ProjectSettingsPage({
  params,
}: {
  params: Promise<{ org: string; project: string }>;
}) {
  return <Settings {...use(params)} />;
}

function Settings({ org, project }: { org: string; project: string }) {
  const router = useRouter();
  const ref = `${org}/${project}`;

  const [current, setCurrent] = useState<Project | null>(null);
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const path = `/projects/${project}`;

  // There is no "get one project" endpoint — the list is the read, and
  // on a single-VPS install it is a handful of rows.
  useEffect(() => {
    if (project) return;
    api
      .get<Project[]>(`/projects`)
      .then((list) => {
        const found = list.find((p) => p.slug === project);
        if (!found) throw new Error("project not found");
        setCurrent(found);
        setDescription(found.description ?? "");
      })
      .catch((e) => setError(message(e)));
  }, [project]);

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

  const dirty = !!current && description !== (current.description ?? "");

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      // PATCH, so sending both is a statement about both and neither is
      // cleared by having been left off the form.
      setCurrent(await api.patch<Project>(path, { description }));
      setSaved(true);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <>
      <Link
        href={`/projects/${project}`}
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
            <TextAreaField
              label="Description"
              hint="Shown on the project's card. Empty is fine."
              rows={3}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={!current}
              placeholder="What this project holds, and who it is for."
            />

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">Slug</Label>
              <div className="flex h-10 items-center border border-border bg-secondary/40 px-3 font-mono text-sm text-muted-foreground">
                {org}/{project}
              </div>
              <p className="text-xs text-subtle-foreground">
                Not editable. It is a path component of every app&apos;s registry reference under
                this project, so renaming it would move every app in it — breaking pushes configured
                against the old path and stranding images already pushed there.
              </p>
            </div>

            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={busy} disabled={!dirty}>
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
              Removes the project, every environment in it and every app inside those. Each
              app&apos;s container is stopped and removed first.
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
        description="Every environment in it goes, and every app in those — containers included. This cannot be undone."
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
