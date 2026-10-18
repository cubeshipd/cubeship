"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowUpRightIcon } from "lucide-react";
import { ErrorAlert } from "@/components/error-alert";
import { InstanceMetrics } from "@/components/instance-metrics";
import { MachineSelector, useMachineSelection } from "@/components/machine-selector";
import Link from "@/components/navigation-link";
import { Notice } from "@/components/notice";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import {
  type App,
  type BackupCoverage,
  type ClusterServer,
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

export default function Overview() {
  const me = useSession();
  const machine = useMachineSelection();
  const projects = useQuery({ ...projectsQuery, enabled: can(me, "projects") });
  const apps = useQuery({ ...appsQuery, enabled: can(me, "apps") });
  const datastores = useQuery({ ...datastoresQuery, enabled: can(me, "databases") });
  const stores = useQuery({ ...storesQuery, enabled: can(me, "storage") });
  const backups = useQuery({ ...backupCoverageQuery, enabled: can(me, "backups") });
  const errors = [projects, apps, datastores, stores, backups, machine.query]
    .flatMap((query) => (query.error ? [message(query.error)] : []))
    .join(" · ");

  return (
    <>
      <SectionHeader
        page
        title="Your infrastructure"
        sub="Resources across your cluster. Monitoring is shown for one node at a time."
        actions={can(me, "servers") ? <MachineSelector selection={machine} /> : undefined}
      />
      <ErrorAlert error={errors || null} />
      <Stats
        me={me}
        projects={projects.data ?? null}
        apps={apps.data ?? null}
        datastores={datastores.data ?? null}
        stores={stores.data ?? null}
        machines={machine.query.data ?? null}
        backups={backups.data ?? null}
      />
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

function Stats({
  me,
  projects,
  apps,
  datastores,
  stores,
  machines,
  backups,
}: {
  me: ReturnType<typeof useSession>;
  projects: Project[] | null;
  apps: App[] | null;
  datastores: Datastore[] | null;
  stores: ObjectStore[] | null;
  machines: ClusterServer[] | null;
  backups: BackupCoverage[] | null;
}) {
  const running = (apps ?? []).filter((a) => a.status === "running").length;
  const ready = (machines ?? []).filter((node) => node.status === "ready").length;
  const targets = backups?.filter((backup) => backup.can_back_up);
  const protectedTargets = targets?.filter((backup) => backup.protected).length;
  const nothingYet =
    projects?.length === 0 &&
    apps?.length === 0 &&
    datastores?.length === 0 &&
    stores?.length === 0;

  return (
    <>
      {nothingYet && can(me, "projects") && (
        <Notice>
          Nothing is deployed on this instance yet.{" "}
          <Link href="/projects" className="text-foreground underline underline-offset-4">
            Create a project
          </Link>{" "}
          to start.
        </Notice>
      )}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {can(me, "projects") && <Stat label="Projects" href="/projects" count={projects?.length} />}
        {can(me, "apps") && (
          <Stat
            label="Apps"
            href="/projects"
            count={apps?.length}
            note={apps && apps.length > 0 ? `${running} running` : undefined}
          />
        )}
        {can(me, "databases") && (
          <Stat label="Databases" href="/databases" count={datastores?.length} />
        )}
        {can(me, "storage") && (
          <Stat label="Object stores" href="/storage" count={stores?.length} />
        )}
        {can(me, "servers") && (
          <Stat
            label="Nodes"
            href="/servers"
            count={machines?.length}
            note={machines ? `${ready} ready` : undefined}
          />
        )}
        {can(me, "backups") && (
          <Stat
            label="Backup targets"
            href="/backups"
            count={targets?.length}
            note={targets ? `${protectedTargets} protected` : undefined}
          />
        )}
      </div>
    </>
  );
}

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
    <Link
      href={href}
      className="group flex min-h-28 flex-col justify-between gap-2 rounded-xl bg-card p-4 text-card-foreground ring-1 ring-foreground/10 transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50"
    >
      <span className="flex items-center justify-between gap-3 text-sm text-muted-foreground">
        {label}
        <ArrowUpRightIcon
          aria-hidden="true"
          className="size-4 shrink-0 text-subtle-foreground group-hover:text-primary"
        />
      </span>
      <span className="text-3xl font-medium tabular-nums tracking-tight">{count ?? "—"}</span>
      {note && <span className="font-mono text-muted-foreground text-xs">{note}</span>}
    </Link>
  );
}
