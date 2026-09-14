import { Star } from "lucide-react";
import Link from "next/link";
import { avatarAt } from "@/lib/avatar";
import type { TemplateSummary } from "@/lib/catalog";
import { formatDate } from "@/lib/dates";
import { VerifiedBadge } from "./verified";

export function TemplateCard({ template }: { template: TemplateSummary }) {
  const updated = new Date(template.release.published_at);
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
          <p className="template-card-repo">
            {template.owner}/{template.name}
          </p>
        </div>
      </div>
      <p className="template-card-description">{template.description}</p>
      <div className="template-card-meta">
        <span className="template-card-owner">
          {/* biome-ignore lint/performance/noImgElement: a GitHub avatar, not one of our own assets. */}
          <img
            src={avatarAt(template.avatar_url, 40)}
            alt=""
            width={20}
            height={20}
            loading="lazy"
            className="size-5 shrink-0 border border-fd-border"
          />
          <span className="truncate">{template.owner}</span>
        </span>
        <span className="template-card-stars">
          <Star className="size-3.5" aria-hidden />
          {template.stars}
        </span>
      </div>
      <div className="template-card-release">
        <span>{template.release.tag}</span>
        <time dateTime={updated.toISOString()}>{formatDate(updated)}</time>
      </div>
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
