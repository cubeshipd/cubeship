import { FolderTreeIcon } from "lucide-react";
import { ResourceCard } from "@/components/resource-grid";
import { StatusRing } from "@/components/status-ring";
import { UsageRings } from "@/components/usage-ring";
import type { App } from "@/lib/api";
import { projectImageSrc } from "@/lib/api";
import type { Shares } from "@/lib/usage";

// One project in the grid: a picture, its name, a ring saying how many
// apps are inside and whether any of them is unhappy, and what those
// apps are taking from the machine between them.
//
// **It used to carry three more things and was worse for it.** The
// environments were badges, which put `production` on every card on the
// screen — a word that is true of everything says nothing about
// anything. The app count was a line of its own, and the states were a
// row of lamps each with a number beside it, which is a legend you read
// rather than a picture you glance at. The count is in the middle of
// the ring now, where it is the same fact in one place.
export function ProjectCard({
  slug,
  hasImage,
  apps,
  shares,
}: {
  slug: string;
  hasImage?: boolean;
  apps: App[];
  shares: Shares;
}) {
  return (
    <ResourceCard
      href={`/projects/${slug}`}
      mark={<ProjectMark slug={slug} hasImage={hasImage} />}
      name={slug}
      detail={`${apps.length} ${apps.length === 1 ? "app" : "apps"}`}
      status={
        <StatusRing
          values={apps.map((a) => a.status)}
          label={`${apps.length} ${apps.length === 1 ? "app" : "apps"} in ${slug}`}
        />
      }
      usage={apps.some((a) => a.has_container) && <UsageRings name={slug} shares={shares} />}
    />
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
