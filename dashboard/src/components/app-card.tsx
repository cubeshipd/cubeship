import { CloudIcon, ContainerIcon, FileCodeIcon, GitBranchIcon } from "lucide-react";
import Link from "@/components/navigation-link";
import { StatusDot } from "@/components/status-badge";
import { UsageBar } from "@/components/usage-bar";
import { type App, type AppSource, hostsOf } from "@/lib/api";
import type { Shares } from "@/lib/usage";

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
// called, where it answers, whether it is up, and what it is using.
//
// **The status is a lamp, not a badge.** A badge is a bordered box with
// a word in it, on a card whose whole job is to be scanned beside eleven
// others. The word is on the app's own page, where there is room to say
// `degraded` and room to say what it means.
export function AppCard({ app, shares }: { app: App; shares: Shares }) {
  const origin = ORIGIN[app.source] ?? { label: app.source, icon: ContainerIcon };
  const Mark = origin.icon;

  return (
    <Link
      href={`/projects/${app.reference}`}
      className="dashboard-resource-card group flex h-full flex-col border border-border bg-card transition-all hover:border-primary/40 hover:bg-secondary/40 focus-visible:border-primary focus-visible:outline-none"
    >
      <span className="dashboard-resource-head flex items-center gap-4">
        <span
          title={origin.label}
          className="flex size-11 shrink-0 items-center justify-center border border-border bg-primary/5 text-primary/70 group-hover:text-primary"
        >
          <Mark className="size-5" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="dashboard-resource-name block truncate font-mono font-medium group-hover:text-primary">
            {app.name}
          </span>
          {/* Empty when it answers nowhere, which is normal for a worker. */}
          <span className="block h-4 truncate font-mono text-[11px] text-muted-foreground">
            {hostsOf(app)}
          </span>
        </span>
        <span className="shrink-0">
          <StatusDot value={app.status} className="size-2" title={app.status} />
        </span>
      </span>
      <span className="dashboard-resource-foot mt-auto grid grid-cols-2 gap-4 border-t border-border">
        <UsageBar label="CPU" percent={shares.cpu} />
        <UsageBar label="Mem" percent={shares.memory} />
      </span>
    </Link>
  );
}
