import { BadgeCheck } from "lucide-react";

// Shown beside a template the catalog vouches for, by who published it.
// The catalog decides (CATALOG_VERIFIED_OWNERS); the site only draws it.
export function VerifiedBadge({ className }: { className: string }) {
  return (
    <span title="Verified publisher" className="inline-flex shrink-0 text-primary">
      <BadgeCheck className={className} aria-label="Verified publisher" role="img" />
    </span>
  );
}
