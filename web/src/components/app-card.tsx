import { CloudIcon, ContainerIcon, FileCodeIcon, GitBranchIcon } from "lucide-react";
import Link from "next/link";
import { StatusDot } from "@/components/status-badge";
import { type App, type AppSource, hostsOf } from "@/lib/api";

// What an app is made of, as a mark and as words.
//
// **The icon carries it instead of a line of text.** It used to be a
// strip across the bottom of every card reading `cubeship registry` —
// four cards deep that is the same four words four times, which is a
// legend rather than a fact about any one of them. As a mark it is in
// the place you already look and costs no line.
//
// The words are still there on hover, because an icon says what it does
// only to somebody who already knows.
const ORIGIN: Record<AppSource, { label: string; icon: typeof ContainerIcon }> = {
  // Pushed to this instance's own registry: the push is the deploy.
  registry: { label: "Cubeship's registry — a push deploys it", icon: ContainerIcon },
  // Somebody else's registry. Nothing tells this instance when one is
  // pushed to, so a deploy is something you ask for — which is the
  // difference worth seeing at a glance.
  external: { label: "An image from another registry", icon: CloudIcon },
  dockerfile: { label: "Built here, from a Dockerfile in a repository", icon: FileCodeIcon },
  railpack: { label: "Built here by Railpack, from a repository", icon: GitBranchIcon },
};

// One app in an environment's grid: what it is made of, what it is
// called, where it answers, and whether it is up.
//
// **The same shape as a project's card**, and for the same reason. It
// was two zones with a rule between them and a footer, which is a lot
// of furniture around four short facts — and the status was a badge, a
// bordered box with a word in it, on a card whose whole job is to be
// scanned beside eleven others. A lamp is the same fact at a glance.
export function AppCard({ app }: { app: App }) {
  const origin = ORIGIN[app.source] ?? { label: app.source, icon: ContainerIcon };
  const Mark = origin.icon;
  const hosts = hostsOf(app);

  return (
    <Link
      href={`/projects/${app.reference}`}
      className="hud-frame group flex items-center gap-4 border border-border bg-card p-4 transition-all hover:border-primary/40 hover:bg-secondary/40 focus-visible:border-primary focus-visible:outline-none"
    >
      <span
        title={origin.label}
        className="flex size-11 shrink-0 items-center justify-center border border-border bg-primary/5 text-primary/70"
      >
        <Mark className="size-5" />
      </span>

      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate font-mono text-sm text-foreground group-hover:text-primary">
          {app.name}
        </span>
        {/* Nothing at all when it answers nowhere, which is the normal
            state for a worker or a queue consumer — not a dash, which
            would read as a name that failed to load. */}
        {hosts && (
          <span className="truncate font-mono text-[11px] text-muted-foreground">{hosts}</span>
        )}
      </span>

      {/* The lamp alone. The word is on the app's own page, where there
          is room to say `degraded` and room to say what it means. */}
      <StatusDot value={app.status} className="size-2" title={app.status} />
    </Link>
  );
}
