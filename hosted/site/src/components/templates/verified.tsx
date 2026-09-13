import { BadgeCheck } from "lucide-react";

// Shown beside a template the catalog vouches for: published by
// Cubeship's own organization. The catalog decides; the site only draws it.
export function VerifiedBadge({ className }: { className: string }) {
  return (
    <span title="Verified: published by Cubeship" className="inline-flex shrink-0 text-primary">
      <BadgeCheck className={className} aria-label="Verified: published by Cubeship" role="img" />
    </span>
  );
}
