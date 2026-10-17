import { BadgeCheckIcon, LayoutTemplateIcon, StarIcon } from "lucide-react";
import Link from "@/components/navigation-link";
import { Badge } from "@/components/ui/badge";
import type { TemplateSummary } from "@/lib/api";

// One template in the catalog: the name, what it does, and enough
// release detail to choose it without opening every card.
export function TemplateCard({ template }: { template: TemplateSummary }) {
  return (
    <Link
      href={`/templates/${template.owner}/${template.name}`}
      className="dashboard-template-card group flex flex-col gap-4 rounded-xl border border-border bg-card p-5 transition-colors hover:border-primary/40 hover:bg-secondary/30 focus-visible:border-primary focus-visible:outline-none"
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
        </div>
      </div>
      <p className="line-clamp-2 min-h-10 text-sm leading-relaxed text-muted-foreground">
        {template.description}
      </p>
      {template.tags.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {template.tags.slice(0, 3).map((tag) => (
            <Badge key={tag} variant="secondary" className="font-normal">
              {tag}
            </Badge>
          ))}
          {template.tags.length > 3 && (
            <span className="px-1.5 py-0.5 text-xs text-muted-foreground">
              +{template.tags.length - 3}
            </span>
          )}
        </div>
      )}
      <div className="mt-auto flex items-center justify-between gap-3 border-t border-border pt-4 text-xs text-muted-foreground">
        <span className="truncate font-mono">{template.release.tag}</span>
        <span className="inline-flex shrink-0 items-center gap-1">
          <StarIcon className="size-3.5" aria-hidden="true" />
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
