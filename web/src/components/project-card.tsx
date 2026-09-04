import Link from "next/link";
import { StatusDot } from "@/components/status-badge";
import { Badge } from "@/components/ui/badge";
import type { App } from "@/lib/api";

// One project in the grid. It carries what you would otherwise have to
// open it to learn: which environments it has, how many apps are in it
// and whether any of them is unhappy.
export function ProjectCard({
  org,
  slug,
  name,
  description,
  environments,
  apps,
}: {
  org: string;
  slug: string;
  name: string;
  description?: string;
  environments: string[];
  apps: App[];
}) {
  // Counted by state rather than listed: on a card, "one failed" is the
  // whole message, and which one is a click away.
  const counts = new Map<string, number>();
  for (const a of apps) counts.set(a.status, (counts.get(a.status) ?? 0) + 1);

  return (
    <Link
      href={`/projects?ref=${org}/${slug}`}
      className="hud-frame group flex flex-col border border-border bg-card transition-all hover:border-primary/40 hover:bg-secondary/40 focus-visible:border-primary focus-visible:outline-none"
    >
      <div className="flex-1 p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h3 className="truncate text-sm font-semibold tracking-[0.1em] uppercase group-hover:text-primary">
              {name}
            </h3>
            <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
              {org}/{slug}
            </p>
            {description && (
              <p className="mt-2.5 line-clamp-2 text-xs leading-relaxed text-muted-foreground">
                {description}
              </p>
            )}
          </div>
        </div>

        <div className="mt-4 flex flex-wrap gap-1.5">
          {environments.length === 0 ? (
            <span className="font-mono text-[11px] text-subtle-foreground">no environments</span>
          ) : (
            environments.map((e) => (
              <Badge key={e} variant="outline" className="font-mono text-muted-foreground">
                {e}
              </Badge>
            ))
          )}
        </div>
      </div>

      <div className="flex items-center justify-between border-t border-border px-4 py-2.5">
        <span className="font-mono text-[11px] text-muted-foreground">
          {apps.length} {apps.length === 1 ? "app" : "apps"}
        </span>
        <span className="flex items-center gap-2.5">
          {[...counts].map(([status, n]) => (
            <span key={status} className="flex items-center gap-1.5">
              <StatusDot value={status} />
              <span className="font-mono text-[11px] text-muted-foreground">{n}</span>
            </span>
          ))}
        </span>
      </div>
    </Link>
  );
}
