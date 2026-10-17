"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowUpRightIcon } from "lucide-react";
import { CubeMark } from "@/components/brand";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal } from "@/components/header-rail";
import { InstanceMetrics } from "@/components/instance-metrics";
import { MachineSelector, useMachineSelection } from "@/components/machine-selector";
import Link from "@/components/navigation-link";
import { Notice } from "@/components/notice";
import { useSession } from "@/components/session-context";
import { StatusDot } from "@/components/status-badge";
import {
  type App,
  type BackupCoverage,
  can,
  type Datastore,
  type ObjectStore,
  type Project,
} from "@/lib/api";
import {
  appsQuery,
  backupCoverageQuery,
  datastoresQuery,
  projectsQuery,
  storesQuery,
} from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

// The screen this instance opens on.
//
// It is about the **instance** rather than about anything in it: what
// is on it, what the machine is doing, and what is using the machine.
// That is the question somebody has before they know
// which project they want — and until this existed the answer to it was
// a page of project cards, which says nothing about whether the box is
// out of disk.
//
// Projects moved to /projects, which is the address they were always
// the index of: an app is /projects/<project>/<env>/<app>.
//
// The order is what somebody scans: what is on this instance, then how
// hard the box is working, then which of the things in the first list is
// the reason.
export default function Overview() {
  const me = useSession();
  const machine = useMachineSelection();
  const projects = useQuery(projectsQuery);
  const apps = useQuery(appsQuery);
  const datastores = useQuery(datastoresQuery);
  const stores = useQuery(storesQuery);
  const backups = useQuery({ ...backupCoverageQuery, enabled: can(me, "backups") });

  return (
    <>
      <RailPortal>
        <MachineSelector selection={machine} />
      </RailPortal>
      <header className="dashboard-overview-intro">
        <div>
          <h1>Your infrastructure.</h1>
          <p>Your cluster at a glance. Monitoring is shown per machine.</p>
        </div>
        <CubeMark className="dashboard-overview-mark" />
      </header>
      <Stats
        projects={projects.data ?? null}
        apps={apps.data ?? null}
        datastores={datastores.data ?? null}
        stores={stores.data ?? null}
      />
      <OperationalStatus
        me={me}
        machines={machine.query.data}
        apps={apps.data}
        datastores={datastores.data}
        stores={stores.data}
        backups={backups.data}
        loaded={
          (!can(me, "servers") || machine.query.isSuccess) &&
          (!can(me, "apps") || apps.isSuccess) &&
          (!can(me, "databases") || datastores.isSuccess) &&
          (!can(me, "storage") || stores.isSuccess) &&
          (!can(me, "backups") || backups.isSuccess)
        }
      />
      <ErrorAlert error={machine.query.error ? message(machine.query.error) : null} />
      {machine.selected && machine.selected.status !== "ready" && (
        <Notice tone="warning">
          {machine.server} is {machine.selected.status}. Historical metrics remain available; live
          readings resume when it reconnects.
        </Notice>
      )}
      <InstanceMetrics server={machine.server} />
    </>
  );
}

function OperationalStatus({
  me,
  machines,
  apps,
  datastores,
  stores,
  backups,
  loaded,
}: {
  me: ReturnType<typeof useSession>;
  machines?: ReturnType<typeof useMachineSelection>["query"]["data"];
  apps?: App[];
  datastores?: Datastore[];
  stores?: ObjectStore[];
  backups?: BackupCoverage[];
  loaded: boolean;
}) {
  if (!loaded) return null;

  const issues = [
    {
      visible: can(me, "servers"),
      singular: "machine waiting or offline",
      plural: "machines waiting or offline",
      count: (machines ?? []).filter((machine) => machine.status !== "ready").length,
      href: "/servers",
    },
    {
      visible: can(me, "apps"),
      singular: "app not serving",
      plural: "apps not serving",
      count: (apps ?? []).filter((app) => app.status === "down" || app.status === "degraded")
        .length,
      href: "/projects",
    },
    {
      visible: can(me, "databases"),
      singular: "database unavailable",
      plural: "databases unavailable",
      count: (datastores ?? []).filter((db) => db.status === "down" || db.status === "failed")
        .length,
      href: "/databases",
    },
    {
      visible: can(me, "storage"),
      singular: "object store unavailable",
      plural: "object stores unavailable",
      count: (stores ?? []).filter((store) => store.status === "down" || store.status === "failed")
        .length,
      href: "/storage",
    },
    {
      visible: can(me, "backups"),
      singular: "backup target needs attention",
      plural: "backup targets need attention",
      count: (backups ?? []).filter(
        (backup) => backup.can_back_up && (!backup.protected || backup.failing),
      ).length,
      href: "/backups",
    },
  ].filter((issue) => issue.visible && issue.count > 0);

  if (!issues.length) return null;

  return (
    <section
      className="mb-6 flex flex-wrap items-center gap-x-4 gap-y-2 border border-warning/30 bg-warning/5 px-4 py-3"
      aria-label="Operational status"
    >
      <span className="inline-flex items-center gap-2 text-sm font-medium">
        <StatusDot value="failed" />
        Needs attention
      </span>
      {issues.map((issue) => (
        <Link
          key={issue.href}
          href={issue.href}
          className="text-xs text-muted-foreground underline decoration-border underline-offset-4 hover:text-foreground"
        >
          {issue.count} {issue.count === 1 ? issue.singular : issue.plural}
        </Link>
      ))}
    </section>
  );
}

// What this instance holds, as four counts.
//
// First on the page, and short on purpose: it is the sentence before the
// charts — how much there is of what the numbers under it are about.
// A count cannot say whether anything is wrong, and does not try to;
// what is wrong is a status on the thing's own screen.
function Stats({
  projects,
  apps,
  datastores,
  stores,
}: {
  projects: Project[] | null;
  apps: App[] | null;
  datastores: Datastore[] | null;
  stores: ObjectStore[] | null;
}) {
  const running = (apps ?? []).filter((a) => a.status === "running").length;
  const nothingYet = projects !== null && projects.length === 0 && (apps ?? []).length === 0;

  return (
    <>
      {nothingYet ? (
        <Notice>
          Nothing is deployed on this instance yet.{" "}
          <Link href="/projects" className="text-foreground underline underline-offset-4">
            Create a project
          </Link>{" "}
          to start.
        </Notice>
      ) : (
        <div className="dashboard-stats">
          <Stat label="Projects" href="/projects" count={projects?.length} />
          <Stat
            label="Apps"
            href="/projects"
            count={apps?.length}
            note={apps && apps.length > 0 ? `${running} running` : undefined}
          />
          <Stat label="Databases" href="/databases" count={datastores?.length} />
          <Stat label="Object stores" href="/storage" count={stores?.length} />
        </div>
      )}
    </>
  );
}

// One count, and the one thing worth saying about it.
//
// Local to this page rather than in src/components: nothing else shows
// a number this way, and a shared component with one caller is a
// decision made before there was anything to decide.
function Stat({
  label,
  href,
  count,
  note,
}: {
  label: string;
  href: string;
  count?: number;
  note?: string;
}) {
  return (
    <Link href={href} className="dashboard-stat">
      <span className="dashboard-stat-label">
        {label}
        <ArrowUpRightIcon aria-hidden="true" />
      </span>
      <span className="dashboard-stat-value">{count ?? "—"}</span>
      <span className="dashboard-stat-note">{note}</span>
    </Link>
  );
}
