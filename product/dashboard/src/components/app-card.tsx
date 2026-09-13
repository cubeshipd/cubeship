import { CloudIcon, ContainerIcon, FileCodeIcon, GitBranchIcon } from "lucide-react";
import { ResourceCard } from "@/components/resource-grid";
import { StatusDot } from "@/components/status-badge";
import { UsageRings } from "@/components/usage-ring";
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
    <ResourceCard
      href={`/projects/${app.reference}`}
      mark={
        <span
          title={origin.label}
          className="flex size-11 shrink-0 items-center justify-center border border-border bg-primary/5 text-primary/70 group-hover:text-primary"
        >
          <Mark className="size-5" />
        </span>
      }
      name={app.name}
      // Empty when it answers nowhere, which is the normal state for a
      // worker — not a dash, which would read as a name that failed to
      // load.
      detail={<span className="font-mono text-[11px]">{hostsOf(app)}</span>}
      status={<StatusDot value={app.status} className="mt-1 size-2" title={app.status} />}
      usage={app.has_container && <UsageRings name={app.name} shares={shares} />}
    />
  );
}
