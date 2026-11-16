"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLinkIcon, FolderTreeIcon, LayersIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { FilterPanel } from "@/components/filter-panel";
import { GLIDE_FIELD_TRIGGER_CLASS, GlideSelect } from "@/components/glide-select";
import Link from "@/components/navigation-link";
import { NewWorkloadAppDialog } from "@/components/new-workload-app-dialog";
import { ActionMenu } from "@/components/resource-header";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { api, can } from "@/lib/api";
import { appsQuery, projectsQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

type Row = {
  slug: string;
  environments: string[];
  apps: number;
  running: number;
};

// What the apps inside a project add up to: all up, none up, or some.
function summary(row: Row): string | null {
  if (row.apps === 0) return null;
  if (row.running === row.apps) return "running";
  return row.running === 0 ? "stopped" : "degraded";
}

export default function ProjectsPage() {
  const router = useRouter();
  const me = useSession();
  const queryClient = useQueryClient();
  const projects = useQuery(projectsQuery);
  const apps = useQuery(appsQuery);
  const [environment, setEnvironment] = useState("");
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);

  const canManageProjects = can(me, "projects", "manage");
  const canCreateProject =
    me.grants === null ||
    me.grants.some(
      (grant) => grant.resource === "projects" && grant.level === "manage" && grant.items === null,
    );
  const canCreate = canCreateProject && can(me, "apps", "manage");

  const rows = useMemo<Row[] | null>(() => {
    if (!projects.data) return null;
    return projects.data.map((project) => {
      const inside = (apps.data ?? []).filter((app) => app.project === project.slug);
      return {
        slug: project.slug,
        environments: project.environments ?? [],
        apps: inside.length,
        running: inside.filter((app) => app.status === "running").length,
      };
    });
  }, [projects.data, apps.data]);

  const environmentChoices = useMemo(
    () => [
      { value: "", label: "All environments" },
      ...[...new Set((rows ?? []).flatMap((row) => row.environments))]
        .sort()
        .map((slug) => ({ value: slug, label: slug })),
    ],
    [rows],
  );
  const filtered = rows?.filter((row) => !environment || row.environments.includes(environment));

  const columns: Column<Row>[] = [
    {
      id: "name",
      header: "Name",
      width: 34,
      sortBy: (row) => row.slug,
      cell: (row) => (
        <span className="flex min-w-0 items-center gap-2.5">
          <FolderTreeIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <span className="truncate font-medium">{row.slug}</span>
        </span>
      ),
    },
    {
      id: "environments",
      header: "Environments",
      width: 28,
      sortBy: (row) => row.environments.length,
      cell: (row) => (
        <span className="truncate text-muted-foreground">
          {row.environments.length ? row.environments.join(", ") : "—"}
        </span>
      ),
    },
    {
      id: "apps",
      header: "Apps",
      width: 14,
      sortBy: (row) => row.apps,
      cell: (row) =>
        apps.isPending ? (
          "…"
        ) : (
          <span className="text-muted-foreground">
            {row.apps ? `${row.running}/${row.apps} running` : "None"}
          </span>
        ),
    },
    {
      id: "status",
      header: "Status",
      width: 14,
      sortBy: (row) => summary(row) ?? "",
      cell: (row) => {
        const value = summary(row);
        return value ? <StatusBadge value={value} /> : "—";
      },
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      cell: (row) => (
        <ActionMenu
          compact
          label={`Actions for ${row.slug}`}
          items={[
            {
              id: "open",
              label: "Open",
              icon: <ExternalLinkIcon className="size-4" />,
              onSelect: () => router.push(`/projects/${row.slug}/production`),
            },
            ...(canManageProjects
              ? [
                  {
                    id: "delete",
                    label: "Delete",
                    icon: <Trash2Icon className="size-4" />,
                    destructive: true,
                    onSelect: () => setDeleting(row.slug),
                  },
                ]
              : []),
          ]}
        />
      ),
    },
  ];

  return (
    <>
      <SectionHeader
        page
        title="Projects"
        sub="Every project on this instance, with the environments and apps inside it."
        actions={
          canCreate ? (
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              New project
            </Button>
          ) : undefined
        }
      />
      <FilterPanel>
        <GlideSelect
          label="Environment"
          value={environment}
          options={environmentChoices}
          onChange={setEnvironment}
          placeholder="All environments"
          searchable
          startIcon={<LayersIcon className="size-4" />}
          className="w-full"
          triggerClassName={GLIDE_FIELD_TRIGGER_CLASS}
        />
      </FilterPanel>
      <ErrorAlert
        error={projects.error ? message(projects.error) : apps.error ? message(apps.error) : null}
      />
      <DataTable
        columns={columns}
        rows={projects.isError ? [] : filtered}
        rowKey={(row) => row.slug}
        onRowClick={(row) => router.push(`/projects/${row.slug}/production`)}
        mobileRow={(row) => {
          const value = summary(row);
          return (
            <article className="py-1">
              <div className="flex items-center gap-2">
                <FolderTreeIcon
                  className="size-4 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <Link
                  href={`/projects/${row.slug}/production`}
                  className="min-w-0 flex-1 truncate text-sm font-medium"
                >
                  {row.slug}
                </Link>
                {value && <StatusBadge value={value} />}
                {columns.find((column) => column.id === "actions")?.cell(row)}
              </div>
              <p className="mt-1 truncate pl-6 text-xs text-muted-foreground">
                {row.apps} {row.apps === 1 ? "app" : "apps"} ·{" "}
                {row.environments.join(", ") || "no environments"}
              </p>
            </article>
          );
        }}
        search={{
          placeholder: "Filter projects",
          countLabel: { singular: "project", plural: "projects" },
          by: (row) => [row.slug, ...row.environments],
        }}
        loadingRows={5}
        empty={
          environment
            ? "No projects match these filters."
            : canCreate
              ? "No projects yet. Create one with its first app."
              : "There are no projects you can use yet. Ask an administrator to create or share one with you."
        }
      />
      <NewWorkloadAppDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={(reference) => {
          setCreating(false);
          void queryClient.invalidateQueries({ queryKey: ["projects"] });
          router.push(`/projects/${reference}`);
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`Delete project ${deleting}?`}
        description="Every environment in it and every app inside those go — containers included. This cannot be undone."
        holdToConfirm
        confirmLabel="Delete project"
        onConfirm={async () => {
          await api.del(`/projects/${deleting}`);
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["apps"] }),
            queryClient.invalidateQueries({ queryKey: ["projects"] }),
          ]);
          setDeleting(null);
        }}
      />
    </>
  );
}
