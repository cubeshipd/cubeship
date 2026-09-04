import Link from "next/link";
import { StatusBadge } from "@/components/status-badge";
import type { App, AppSource } from "@/lib/api";

const ORIGIN: Record<AppSource, string> = {
  registry: "cubeship registry",
  external: "external image",
  dockerfile: "github · dockerfile",
  railpack: "github · railpack",
};

// One app in an environment's grid. The name is the part you scan for,
// so it leads; the reference is on the app's own page.
export function AppCard({ app }: { app: App }) {
  return (
    <Link
      href={`/apps?ref=${app.reference}`}
      className="hud-frame group flex flex-col border border-border bg-card transition-all hover:border-primary/40 hover:bg-secondary/40 focus-visible:border-primary focus-visible:outline-none"
    >
      <div className="flex-1 p-4">
        <div className="flex items-start justify-between gap-3">
          <h3 className="min-w-0 truncate font-mono text-sm text-foreground group-hover:text-primary">
            {app.name}
          </h3>
          <StatusBadge value={app.status} />
        </div>
        <p className="mt-2 truncate font-mono text-[11px] text-muted-foreground">
          {app.domain || "no domain"}
        </p>
      </div>

      <div className="border-t border-border px-4 py-2.5">
        <span className="font-mono text-[10px] tracking-[0.16em] text-subtle-foreground uppercase">
          {/* What the app is made of decides what you do next — push to
              it, or point a deploy at a commit — so it is on the card
              rather than one level in. */}
          {ORIGIN[app.source] ?? app.source}
        </span>
      </div>
    </Link>
  );
}
