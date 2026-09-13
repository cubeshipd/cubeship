import { FolderTreeIcon } from "lucide-react";
import Link from "next/link";
import { StatusRing } from "@/components/status-ring";
import type { App } from "@/lib/api";
import { projectImageSrc } from "@/lib/api";

// One project in the grid: a picture, its name, and a ring saying how
// many apps are inside and whether any of them is unhappy.
//
// **It used to carry three more things and was worse for it.** The
// environments were badges, which put `production` on every card on the
// screen — a word that is true of everything says nothing about
// anything. The app count was a line of its own, and the states were a
// row of lamps each with a number beside it, which is a legend you read
// rather than a picture you glance at. The count is in the middle of
// the ring now, where it is the same fact in one place.
//
// What is left is what makes a grid worth having over a list: something
// to recognise, a name, and whether to open it.
export function ProjectCard({
  slug,
  hasImage,
  apps,
}: {
  slug: string;
  hasImage?: boolean;
  apps: App[];
}) {
  return (
    <Link
      href={`/projects/${slug}`}
      className="hud-frame group flex items-center gap-4 border border-border bg-card p-4 transition-all hover:border-primary/40 hover:bg-secondary/40 focus-visible:border-primary focus-visible:outline-none"
    >
      <ProjectMark slug={slug} hasImage={hasImage} />
      <h3 className="min-w-0 flex-1 truncate font-mono text-sm font-semibold group-hover:text-primary">
        {slug}
      </h3>
      <StatusRing
        values={apps.map((a) => a.status)}
        label={`${apps.length} ${apps.length === 1 ? "app" : "apps"} in ${slug}`}
      />
    </Link>
  );
}

// The picture, or the mark a project wears until somebody chooses one.
//
// **One mark and not a colour per project.** A tint derived from the
// slug would be a colour that means nothing on an interface where green,
// amber and red already mean state — and the thing that tells two
// projects apart is the picture, which is what the settings screen is
// for.
export function ProjectMark({
  slug,
  hasImage,
  className = "size-11",
}: {
  slug: string;
  hasImage?: boolean;
  className?: string;
}) {
  if (hasImage) {
    return (
      // biome-ignore lint/performance/noImgElement: served by the daemon, not by Next
      <img
        src={projectImageSrc(slug)}
        alt=""
        className={`${className} shrink-0 border border-border object-cover`}
      />
    );
  }
  return (
    <span
      className={`${className} flex shrink-0 items-center justify-center border border-border bg-primary/5 text-primary/70`}
    >
      <FolderTreeIcon className="size-5" />
    </span>
  );
}
