import { Star } from "lucide-react";
import Link from "next/link";
import { avatarAt } from "@/lib/avatar";
import { formatDate } from "@/lib/dates";
import type { CatalogRow } from "@/lib/templates/queries";

export function TemplateCard({ template }: { template: CatalogRow }) {
  return (
    <Link
      href={`/templates/${template.owner}/${template.name}`}
      className="hud-frame group flex flex-col border border-border bg-card p-4 transition-colors hover:border-primary"
    >
      <div className="flex items-start gap-3">
        <TemplateIcon iconKey={template.iconKey} className="size-12" />
        <div className="min-w-0">
          <h3 className="truncate font-medium text-fd-foreground">{template.title}</h3>
          <p className="truncate font-mono text-subtle-foreground text-xs">
            {template.owner}/{template.name}
          </p>
        </div>
      </div>
      <p className="mt-3 line-clamp-2 min-h-10 text-fd-muted-foreground text-sm">
        {template.description}
      </p>
      <div className="mt-4 flex items-center justify-between text-fd-muted-foreground text-xs">
        <span className="flex min-w-0 items-center gap-2">
          {/* biome-ignore lint/performance/noImgElement: a GitHub avatar, not one of our own assets. */}
          <img
            src={avatarAt(template.avatarUrl, 40)}
            alt=""
            width={20}
            height={20}
            loading="lazy"
            className="size-5 shrink-0 border border-fd-border"
          />
          <span className="truncate">{template.owner}</span>
        </span>
        <span className="flex items-center gap-1">
          <Star className="size-3.5" aria-hidden />
          {template.stars}
        </span>
      </div>
      <p className="label mt-2 text-subtle-foreground">
        Updated{" "}
        <time dateTime={template.publishedAt.toISOString()}>
          {formatDate(template.publishedAt)}
        </time>
      </p>
    </Link>
  );
}

// A fixed box whether or not the icon has loaded, so nothing moves.
export function TemplateIcon({
  iconKey,
  className,
}: {
  iconKey: string | null;
  className: string;
}) {
  return (
    <div className={`${className} shrink-0 overflow-hidden border border-fd-border bg-grid`}>
      {iconKey ? (
        // biome-ignore lint/performance/noImgElement: re-encoded by discovery and served from our own path.
        <img src={`/i/${iconKey}`} alt="" className="h-full w-full object-cover" />
      ) : null}
    </div>
  );
}
