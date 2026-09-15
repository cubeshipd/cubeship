import { BadgeCheckIcon, LayoutTemplateIcon, StarIcon } from "lucide-react";
import Link from "@/components/navigation-link";
import type { TemplateSummary } from "@/lib/api";

// One template in the grid: its icon, name, who published it and what
// it is. Opening it is where the install is.
export function TemplateCard({ template }: { template: TemplateSummary }) {
  return (
    <Link
      href={`/templates/${template.owner}/${template.name}`}
      className="dashboard-template-card group flex flex-col gap-4 border border-border bg-card p-5 transition-all hover:border-primary/40 hover:bg-secondary/40 focus-visible:border-primary focus-visible:outline-none"
    >
      <div className="flex items-center gap-3">
        <TemplateMark src={template.icon_url} className="size-11" />
        <div className="min-w-0 flex-1">
          <h3 className="flex items-center gap-1.5 text-sm font-semibold group-hover:text-primary">
            <span className="truncate">{template.title}</span>
            {template.verified && (
              <BadgeCheckIcon
                className="size-4 shrink-0 text-primary"
                aria-label="Verified publisher"
              />
            )}
          </h3>
          <p className="truncate font-mono text-xs text-subtle-foreground">
            {template.owner}/{template.name}
          </p>
        </div>
      </div>
      <p className="line-clamp-2 min-h-10 text-sm text-muted-foreground">{template.description}</p>
      <div className="flex items-center justify-between font-mono text-xs text-subtle-foreground">
        <span>{template.release.tag}</span>
        <span className="flex items-center gap-1">
          <StarIcon className="size-3.5" aria-hidden />
          {template.stars}
        </span>
      </div>
    </Link>
  );
}

// The icon, or a mark in its place, in a box of the same size either way.
export function TemplateMark({ src, className }: { src: string | null; className: string }) {
  return (
    <div
      className={`${className} flex shrink-0 items-center justify-center overflow-hidden border border-border bg-background`}
    >
      {src ? (
        // biome-ignore lint/performance/noImgElement: served by the daemon, which next/image has no loader for.
        <img src={src} alt="" className="size-full object-cover" />
      ) : (
        <LayoutTemplateIcon className="size-5 text-subtle-foreground" />
      )}
    </div>
  );
}
