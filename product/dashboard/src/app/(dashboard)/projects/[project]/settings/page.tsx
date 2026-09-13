"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useEffect, useRef, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { ProjectMark } from "@/components/project-card";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api, type Project } from "@/lib/api";
import { message } from "@/lib/errors";

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
      <ProjectImage project={project} />
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

// The size a project's picture is stored at.
//
// **The browser is what scales it**, and it is the only place that can
// without a decoder on the daemon: the file is already decoded here to
// be previewed. So what crosses the wire is a square of a few tens of
// kilobytes rather than whatever came off somebody's phone, and the
// daemon's job is to bound and sniff rather than to resize.
//
// 256 because the largest this is ever drawn is the mark on a card, at
// 44px — twice that on a retina screen, and a little room for the day
// it is drawn larger.
const IMAGE_SIZE = 256;

// square draws the middle of an image into a square canvas and hands
// back a PNG.
//
// Cover-cropped rather than letterboxed: a picture with bars around it
// is one that reads as broken in a grid of things that fill their
// frames, and the middle is where the subject of a photo is.
async function square(file: File): Promise<Blob> {
  const bitmap = await createImageBitmap(file);
  const side = Math.min(bitmap.width, bitmap.height);
  const canvas = document.createElement("canvas");
  canvas.width = IMAGE_SIZE;
  canvas.height = IMAGE_SIZE;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("this browser cannot resize the image");
  ctx.drawImage(
    bitmap,
    (bitmap.width - side) / 2,
    (bitmap.height - side) / 2,
    side,
    side,
    0,
    0,
    IMAGE_SIZE,
    IMAGE_SIZE,
  );
  bitmap.close();
  return new Promise((resolve, reject) =>
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error("the image could not be encoded"))),
      "image/png",
    ),
  );
}

// The picture a project wears in the grid.
//
// **A file picker and nothing that looks like a form.** There is one
// value, it applies the moment it is chosen, and a Save button beside a
// picture already on screen would be asking somebody to confirm what
// they can see.
function ProjectImage({ project }: { project: string }) {
  const me = useSession();
  const [has, setHas] = useState<boolean | null>(null);
  // Bumped after every write, so the <img> asks again. The daemon sends
  // `no-cache` and an ETag, so the ask is cheap and the answer is the
  // new bytes or a 304 — but nothing tells React to re-render the same
  // src on its own.
  const [version, setVersion] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const file = useRef<HTMLInputElement>(null);

  useEffect(() => {
    api
      .get<Project[]>("/projects")
      .then((all) => setHas(all.find((p) => p.slug === project)?.has_image ?? false))
      .catch((e) => setError(message(e)));
  }, [project]);

  async function choose(picked: File | undefined) {
    if (!picked) return;
    setBusy(true);
    setError(null);
    try {
      await api.putBytes(`/projects/${project}/image`, await square(picked));
      setHas(true);
      setVersion((v) => v + 1);
    } catch (e) {
      setError(message(e));
    }
    setBusy(false);
    // So picking the same file twice is two events rather than one.
    if (file.current) file.current.value = "";
  }

  async function clear() {
    setBusy(true);
    setError(null);
    try {
      await api.del(`/projects/${project}/image`);
      setHas(false);
      setVersion((v) => v + 1);
    } catch (e) {
      setError(message(e));
    }
    setBusy(false);
  }

  // A member sees the project and does not decide what it looks like to
  // everybody else. Shown rather than hidden, because what it is is
  // still worth seeing.
  const mine = me.role === "admin";

  return (
    <>
      <SectionHeader
        title="Picture"
        sub="What this project wears on the projects grid. Cropped to a square and scaled down here, before it is sent."
      />
      <Card className="mb-6">
        <CardContent>
          <ErrorAlert error={error} />
          <div className="flex items-center gap-5">
            <ProjectMark key={version} slug={project} hasImage={has === true} className="size-20" />
            <div className="min-w-0 space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <input
                  ref={file}
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  className="hidden"
                  onChange={(e) => choose(e.target.files?.[0])}
                />
                <ActionButton
                  busy={busy}
                  variant="outline"
                  disabled={!mine}
                  onClick={() => file.current?.click()}
                >
                  {has ? "Replace" : "Choose a picture"}
                </ActionButton>
                {has && mine && (
                  <Button variant="ghost" disabled={busy} onClick={clear}>
                    Remove
                  </Button>
                )}
              </div>
              <p className="text-[11px] text-muted-foreground">
                {mine
                  ? "PNG, JPEG or WebP. With none, the project wears a mark."
                  : "Choosing one is an admin's: it is what this project looks like to everybody on the instance."}
              </p>
            </div>
          </div>
        </CardContent>
      </Card>
    </>
  );
}
