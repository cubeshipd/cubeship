"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { type Column, DataTable } from "@/components/data-table";
import { InstanceMetrics } from "@/components/instance-metrics";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { Card, CardContent } from "@/components/ui/card";
import {
  type App,
  api,
  type ContainerUsage,
  type Datastore,
  formatBytes,
  formatCPU,
  type ObjectStore,
  type Project,
} from "@/lib/api";

// The screen this instance opens on.
//
// It is about the **instance** rather than about anything in it: what
// is wrong, what the machine is doing, what is using it, and what is
// deployed on it. That is the question somebody has before they know
// which project they want — and until this existed the answer to it was
// a page of project cards, which says nothing about whether the box is
// out of disk.
//
// Projects moved to /projects, which is the address they were always
// the index of: an app is /projects/<project>/<env>/<app>.
//
// The order is what somebody scans, not what is most interesting to
// build. Anything down comes first and only when there is something —
// an alert nobody has is a heading everybody learns to skip. Then the
// machine, then what is using it, then the inventory.
export default function Overview() {
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [apps, setApps] = useState<App[] | null>(null);
  const [datastores, setDatastores] = useState<Datastore[] | null>(null);
  const [stores, setStores] = useState<ObjectStore[] | null>(null);

  // Each on its own, and a failure is that one card saying nothing
  // rather than the screen saying nothing. A member may be refused one
  // of these lists on an instance where they can read the rest, and a
  // landing page must survive that.
  useEffect(() => {
    api
      .get<Project[]>("/projects")
      .then(setProjects)
      .catch(() => setProjects([]));
    api
      .get<App[]>("/apps")
      .then(setApps)
      .catch(() => setApps([]));
    api
      .get<Datastore[]>("/datastores")
      .then(setDatastores)
      .catch(() => setDatastores([]));
    api
      .get<ObjectStore[]>("/objectstores")
      .then(setStores)
      .catch(() => setStores([]));
  }, []);

  // What counts as wrong, per kind, and it is not simply "not running".
  //
  // An app that has never been deployed is `pending`, which is a normal
  // state for a normal app — nothing was asked of it yet. A database
  // somebody stopped is `stopped`, which is a decision rather than a
  // fault; `down` is the one that means its container went away on its
  // own. Flagging either would train people to ignore this list, which
  // is the only failure mode that matters for it.
  const troubled = [
    ...(apps ?? [])
      .filter((a) => a.status === "down")
      .map((a) => ({
        kind: "App",
        name: a.reference,
        href: `/projects/${a.reference}`,
        status: a.status,
      })),
    ...(datastores ?? [])
      .filter((d) => d.status === "down" || d.status === "failed")
      .map((d) => ({
        kind: "Database",
        name: d.name,
        href: `/databases/${d.name}`,
        status: d.status,
      })),
    ...(stores ?? [])
      .filter((s) => s.status === "down" || s.status === "failed")
      .map((s) => ({
        kind: "Storage",
        name: s.name,
        href: `/storage/${s.name}`,
        status: s.status,
      })),
  ];

  return (
    <>
      <PageHeader title="Overview" />

      {troubled.length > 0 && (
        <Card className="mb-6 border-l-2 border-l-destructive">
          <CardContent className="divide-y divide-border p-0">
            {troubled.map((t) => (
              <Link
                key={`${t.kind}:${t.name}`}
                href={t.href}
                className="flex items-center justify-between gap-4 px-4 py-3 transition-colors hover:bg-secondary/50"
              >
                <span className="flex min-w-0 items-baseline gap-3">
                  <span className="w-20 shrink-0 text-[11px] tracking-[0.12em] text-subtle-foreground uppercase">
                    {t.kind}
                  </span>
                  {/* The full reference, never the bare name: `gateway`
                      is unique inside one environment and nowhere else,
                      so a list of names across the instance would be a
                      list of things it does not identify. */}
                  <span className="truncate font-mono text-sm">{t.name}</span>
                </span>
                <StatusBadge value={t.status} />
              </Link>
            ))}
          </CardContent>
        </Card>
      )}

      <InstanceMetrics />
      <Containers />
      <Deployed projects={projects} apps={apps} datastores={datastores} stores={stores} />
    </>
  );
}

// How often the list of what is using the box is re-read. The daemon
// samples every 30 seconds, so anything faster is two requests for one
// reading.
const REFRESH_MS = 30_000;

// What every container on this instance is using, heaviest first.
//
// The counterpart to the charts above it: those say the box is at 80%,
// and this says which of the twenty things on it is the reason. It
// crosses apps, databases and object stores, because the question does
// — and it is the one screen in the dashboard that does, which is why
// it is served at the instance's own address rather than any module's.
function Containers() {
  const [usage, setUsage] = useState<ContainerUsage[] | null>(null);
  const [refused, setRefused] = useState(false);

  useEffect(() => {
    const load = () =>
      api
        .get<ContainerUsage[]>("/instance/containers")
        .then((found) => {
          setUsage(found);
          setRefused(false);
        })
        .catch(() => {
          setUsage([]);
          setRefused(true);
        });
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => clearInterval(timer);
  }, []);

  const columns: Column<ContainerUsage>[] = [
    {
      id: "name",
      header: "Container",
      width: 56,
      sortBy: (u) => u.name,
      cell: (u) => (
        <span className="flex min-w-0 items-baseline gap-3">
          <span className="w-20 shrink-0 text-[11px] tracking-[0.12em] text-subtle-foreground uppercase">
            {kinds[u.kind] ?? u.kind}
          </span>
          <span className="truncate font-mono text-sm">{u.name}</span>
        </span>
      ),
    },
    {
      id: "cpu",
      header: "CPU",
      width: 22,
      align: "right",
      sortBy: (u) => u.cpu_percent,
      cell: (u) => <span className="font-mono text-sm">{formatCPU(u.cpu_percent)}</span>,
    },
    {
      id: "memory",
      header: "Memory",
      width: 22,
      align: "right",
      sortBy: (u) => u.memory_bytes,
      cell: (u) => <span className="font-mono text-sm">{formatBytes(u.memory_bytes)}</span>,
    },
  ];

  return (
    <>
      <SectionHeader
        title="Containers"
        // The convention is the one thing about this number that
        // surprises people, and it is the opposite of the chart above.
        sub="What each is using right now, heaviest first. 100% is one core here — not the whole machine, as it is above. Sort by memory for the other question."
      />
      <DataTable
        columns={columns}
        rows={usage}
        rowKey={(u) => `${u.kind}:${u.name}`}
        onRowClick={(u) => {
          const href = hrefFor(u);
          if (href) window.location.assign(href);
        }}
        loadingRows={4}
        // Bounded, because an instance with thirty containers would
        // otherwise be a page that is the list.
        maxHeight="24rem"
        empty={
          refused
            ? "This account may not read what the instance is running."
            : "Nothing has been sampled yet — the first reading lands within a minute of a container starting."
        }
        className="mb-4"
      />
    </>
  );
}

// What a kind is called on screen. The daemon's words are the module
// names; these are the product's.
const kinds: Record<string, string> = {
  app: "App",
  datastore: "Database",
  objectstore: "Storage",
};

// Where a row goes. Derived here rather than served, because it is a
// fact about this dashboard's routes and not about the daemon.
function hrefFor(u: ContainerUsage): string | null {
  switch (u.kind) {
    case "app":
      return `/projects/${u.name}`;
    case "datastore":
      return `/databases/${u.name}`;
    case "objectstore":
      return `/storage/${u.name}`;
    default:
      return null;
  }
}

// What is deployed here, as four counts.
//
// Context rather than the point — the point is above it. A number alone
// cannot say whether anything is wrong, which is what the list at the
// top of the page is for.
function Deployed({
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
      <SectionHeader title="Deployed here" />

      {nothingYet ? (
        <Notice>
          Nothing is deployed on this instance yet.{" "}
          <Link href="/projects" className="text-foreground underline underline-offset-4">
            Create a project
          </Link>{" "}
          to start.
        </Notice>
      ) : (
        <div className="mb-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
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
    <Card className="transition-colors hover:border-border-strong">
      <CardContent>
        <Link href={href} className="block">
          <div className="text-[11px] tracking-[0.12em] text-muted-foreground uppercase">
            {label}
          </div>
          {/* An em dash while it loads, and after a refusal. A zero
              would be a claim. */}
          <div className="mt-1 font-mono text-2xl text-foreground">{count ?? "—"}</div>
          <div className="mt-1 h-4 text-[11px] text-subtle-foreground">{note}</div>
        </Link>
      </CardContent>
    </Card>
  );
}
