"use client";

import { useQuery } from "@tanstack/react-query";
import {
  ArrowUpRightIcon,
  ChevronDownIcon,
  PackageCheckIcon,
  TagsIcon,
  Trash2Icon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import Link from "@/components/navigation-link";
import { StatusBadge } from "@/components/status-badge";
import StatusMark from "@/components/status-mark";
import { TemplateUninstallDialog } from "@/components/template-uninstall-dialog";
import { Button } from "@/components/ui/button";
import { api, type TemplateInstall, type TemplateSummary } from "@/lib/api";
import { message } from "@/lib/errors";
import { fetchTemplates } from "@/lib/templates";

// What a badge says about an installation: a run in progress says what it
// is doing, rather than the state it is about to leave.
export function installStatus(i: TemplateInstall): string {
  const run = i.runs[0];
  if (i.busy && run) {
    return { install: "installing", update: "updating", uninstall: "uninstalling" }[run.kind];
  }
  return i.status;
}

// The Installed tab: every template on this instance and its uninstall action.
export function TemplateInstalls() {
  const router = useRouter();
  const [uninstalling, setUninstalling] = useState<TemplateInstall | null>(null);
  const [category, setCategory] = useState("all");
  const installs = useQuery({
    queryKey: ["template-installs"],
    queryFn: () => api.get<TemplateInstall[]>("/template-installs"),
    // Only while something is changing: the list is otherwise still.
    refetchInterval: (q) => (q.state.data?.some((i) => i.busy) ? 3000 : false),
  });
  const catalog = useQuery({
    queryKey: ["templates", "all", "", "all"],
    queryFn: ({ signal }) => fetchTemplates("", "all", signal),
    staleTime: 60_000,
    retry: 1,
    enabled: Boolean(installs.data?.length),
  });

  const catalogByName = new Map(
    (catalog.data ?? []).map((template) => [templateKey(template), template]),
  );
  const installTags = new Map<number, string[]>();
  const categoryCounts = new Map<string, number>();
  for (const install of installs.data ?? []) {
    const tags = catalogByName.get(`${install.owner}/${install.repo}`)?.tags ?? [];
    installTags.set(install.id, tags);
    for (const tag of tags) categoryCounts.set(tag, (categoryCounts.get(tag) ?? 0) + 1);
  }
  const categoryOptions = [
    {
      value: "all",
      label: <CategoryOption name="All categories" count={installs.data?.length ?? 0} />,
      searchText: "All categories",
    },
    ...[...categoryCounts]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([tag, count]) => ({
        value: tag,
        label: <CategoryOption name={tag} count={count} />,
        searchText: tag,
      })),
  ];

  return (
    <>
      <ErrorAlert error={installs.error ? message(installs.error) : null} />
      {(installs.data || installs.isLoading) && (
        <DataTable
          columns={columns(setUninstalling)}
          rows={installs.data ?? null}
          rowKey={(install) => String(install.id)}
          search={{
            placeholder: "Search installed templates",
            countLabel: { singular: "template", plural: "templates" },
            by: (install) => [install.name, install.project, install.environment, install.release],
            filter: (install) =>
              category === "all" || installTags.get(install.id)?.includes(category) === true,
            trailing: (
              <GlideSelect
                ariaLabel="Filter installed templates by category"
                placeholder="All categories"
                startIcon={<TagsIcon className="size-4" />}
                triggerContent={
                  <>
                    <span className="flex min-w-0 items-center gap-2">
                      <TagsIcon className="size-4 shrink-0 text-primary" aria-hidden="true" />
                      <span className="truncate">
                        {category === "all" ? "All categories" : category}
                      </span>
                    </span>
                    <ChevronDownIcon
                      className="size-4 shrink-0 text-muted-foreground"
                      aria-hidden="true"
                    />
                  </>
                }
                options={categoryOptions}
                value={category}
                onChange={setCategory}
                searchable
                busy={catalog.isLoading}
                emptyMessage="No categories found."
                className="w-56 shrink-0"
                triggerClassName="flex h-[38px] w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              />
            ),
          }}
          empty={
            <div className="flex min-h-56 flex-col items-center justify-center px-6 py-10 text-center">
              <span className="mb-4 grid size-12 place-items-center rounded-xl border border-border bg-background text-muted-foreground">
                <PackageCheckIcon className="size-5" />
              </span>
              <h2 className="text-base font-medium">No installed templates</h2>
              <p className="mt-1 max-w-sm text-sm text-muted-foreground">
                Templates you install will be listed here.
              </p>
              <Button variant="outline" className="mt-5" onClick={() => router.push("/templates")}>
                Browse catalog
              </Button>
            </div>
          }
        />
      )}
      {uninstalling && (
        <TemplateUninstallDialog
          install={uninstalling}
          open
          onOpenChange={(open) => !open && setUninstalling(null)}
        />
      )}
    </>
  );
}

function templateKey(template: TemplateSummary) {
  return `${template.owner}/${template.name}`;
}

function CategoryOption({ name, count }: { name: string; count: number }) {
  return (
    <span className="flex w-full items-center justify-between gap-4">
      <span className="truncate">{name}</span>
      <span className="shrink-0 font-mono text-xs text-muted-foreground">{count}</span>
    </span>
  );
}

function columns(setUninstalling: (install: TemplateInstall) => void): Column<TemplateInstall>[] {
  return [
    {
      id: "template",
      header: "Template",
      width: 54,
      sortBy: (install) => install.name,
      cell: (install) => (
        <div className="min-w-0">
          <Link
            href={`/templates/installs/${install.id}`}
            className="group inline-flex max-w-full items-center gap-1.5 font-mono text-sm font-medium hover:text-primary"
          >
            <span className="truncate">{install.name}</span>
            <ArrowUpRightIcon className="size-3.5 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
          </Link>
          <p className="truncate font-mono text-xs text-muted-foreground">
            {install.project}/{install.environment}
          </p>
        </div>
      ),
    },
    {
      id: "release",
      header: "Release",
      width: 36,
      sortBy: (install) => install.release,
      cell: (install) => {
        const status = installStatus(install);
        const exceptional = install.busy || install.status !== "installed";
        if (install.busy && install.runs[0]?.status === "running") {
          return (
            <div className="space-y-1">
              <span className="font-mono text-xs">{install.release}</span>
              <StatusMark status="running" label={status} size={16} fontSize={12} />
            </div>
          );
        }
        return (
          <div className="space-y-1">
            <span className="font-mono text-xs">{install.release}</span>
            {exceptional && <StatusBadge value={status} />}
          </div>
        );
      },
    },
    {
      id: "actions",
      header: "",
      width: 10,
      align: "right",
      cell: (install) => (
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={`Uninstall ${install.name}`}
          disabled={install.busy || install.status !== "installed"}
          onClick={() => setUninstalling(install)}
        >
          <Trash2Icon className="text-destructive" />
        </Button>
      ),
    },
  ];
}
