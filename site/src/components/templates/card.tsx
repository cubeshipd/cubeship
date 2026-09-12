import { Heart } from "lucide-react";
import Link from "next/link";
import type { CatalogRow } from "@/lib/templates/queries";

// One mono line of what a template creates: the app count, and the
// engines if it wires up any databases, the way an install summary
// would read it.
function summarize(creates: CatalogRow["creates"]): string {
  const apps = `${creates.apps} app${creates.apps === 1 ? "" : "s"}`;
  return creates.engines.length > 0 ? `${apps} · ${creates.engines.join(", ")}` : apps;
}

export function TemplateCard({ template }: { template: CatalogRow }) {
  return (
    <Link
      href={`/templates/${template.slug}`}
      className="hud-frame group block border border-border bg-card transition-colors hover:border-primary"
    >
      <div className="aspect-video overflow-hidden border-border border-b bg-grid">
        {template.imageKey ? (
          // A photo the author uploaded, re-encoded and served from our
          // own path — not a remote domain next/image needs configured for.
          // eslint-disable-next-line @next/next/no-img-element
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
          <span>{template.author.login}</span>
          <span className="flex items-center gap-1">
            <Heart className="size-3.5" aria-hidden />
            {template.likesCount}
          </span>
        </div>
        <p className="label text-subtle-foreground">{summarize(template.creates)}</p>
      </div>
    </Link>
  );
}
