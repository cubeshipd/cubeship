"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";

export default function ProjectSettingsPage({ params }: PageProps<"/projects/[project]/settings">) {
  return <Settings {...use(params)} />;
}

// A project's settings, which is one irreversible act and nothing else.
//
// There was a General section above it whose whole content was the
// description, plus the slug shown read-only. Both are gone — the
// description everywhere, and the slug because a value you cannot edit,
// on a page you opened to edit something, is a field that teaches people
// settings screens are where facts live. The slug is in the URL and in
// the header of every page under it.
//
// The page stays rather than becoming a delete button in the project's
// header, for the reason it always existed: destroying a thing belongs
// at the bottom of a page you went to on purpose.
function Settings({ project }: { project: string }) {
  const router = useRouter();
  const [deleting, setDeleting] = useState(false);

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

  return (
    <>
      {" "}
      <PageHeader title="Project settings" />
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
          await api.del(`/projects/${project}`);
          router.push("/projects");
        }}
      />
    </>
  );
}
