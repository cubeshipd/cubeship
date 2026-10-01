import Link from "next/link";
import type { TemplateSummary } from "@/lib/catalog";
import { VerifiedBadge } from "./verified";

export function TemplateCard({ template }: { template: TemplateSummary }) {
  return (
    <Link
      href={`/templates/${template.owner}/${template.name}`}
      className="template-card hud-frame group"
    >
      <div className="template-card-heading">
        <TemplateIcon src={template.icon_url} className="size-14" />
        <div className="min-w-0 flex-1">
          <h3>
            <span className="truncate">{template.title}</span>
            {template.verified ? <VerifiedBadge className="size-4" /> : null}
          </h3>
        </div>
      </div>
      <p className="template-card-description">{template.description}</p>
    </Link>
  );
}

// A fixed box whether or not the icon has loaded, so nothing moves.
export function TemplateIcon({ src, className }: { src: string | null; className: string }) {
  return (
    <div className={`${className} shrink-0 overflow-hidden border border-fd-border bg-grid`}>
      {src ? (
        // biome-ignore lint/performance/noImgElement: re-encoded by the catalog and cached forever at its commit's address.
        <img src={src} alt="" className="h-full w-full object-cover" />
      ) : null}
    </div>
  );
}
