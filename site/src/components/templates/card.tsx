import { Heart } from "lucide-react";
import Link from "next/link";
import { avatarAt } from "@/lib/avatar";
import type { CatalogRow } from "@/lib/templates/queries";

// UTC, so the server and the browser print the same day.
const dateFormat = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeZone: "UTC" });

export function TemplateCard({ template }: { template: CatalogRow }) {
  return (
    <Link
      href={`/templates/${template.slug}`}
      className="hud-frame group block border border-border bg-card transition-colors hover:border-primary"
    >
      <div className="aspect-video overflow-hidden border-border border-b bg-grid">
        {template.imageKey ? (
          // biome-ignore lint/performance/noImgElement: a photo the author uploaded, re-encoded and served from our own path — not a remote domain next/image needs configured for.
          <img
            src={`/i/${template.imageKey}`}
            alt=""
            loading="lazy"
            className="h-full w-full object-cover transition-transform group-hover:scale-[1.02]"
          />
        ) : null}
      </div>
      <div className="space-y-2 p-4">
        <h3 className="font-medium text-fd-foreground">{template.name}</h3>
        <p className="line-clamp-2 text-fd-muted-foreground text-sm">{template.summary}</p>
        <div className="flex items-center justify-between pt-1 text-fd-muted-foreground text-xs">
          <span className="flex min-w-0 items-center gap-2">
            {template.author.avatarUrl ? (
              // biome-ignore lint/performance/noImgElement: a GitHub avatar, not one of our own assets.
              <img
                src={avatarAt(template.author.avatarUrl, 40)}
                alt=""
                width={20}
                height={20}
                loading="lazy"
                className="size-5 shrink-0 border border-fd-border"
              />
            ) : null}
            <span className="truncate">{template.author.login}</span>
          </span>
          <span className="flex items-center gap-1">
            <Heart className="size-3.5" aria-hidden />
            {template.likesCount}
          </span>
        </div>
        <p className="label text-subtle-foreground">
          Updated{" "}
          <time dateTime={template.updatedAt.toISOString()}>
            {dateFormat.format(template.updatedAt)}
          </time>
        </p>
      </div>
    </Link>
  );
}
