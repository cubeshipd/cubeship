"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowUpCircleIcon, Trash2Icon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { RowAction, RowActions } from "@/components/row-actions";
import { StatusBadge } from "@/components/status-badge";
import { TemplateUninstallDialog } from "@/components/template-uninstall-dialog";
import { TemplateUpdateDialog } from "@/components/template-update-dialog";
import { api, type TemplateInstall } from "@/lib/api";
import { message } from "@/lib/errors";

// What a badge says about an installation: a run in progress says what it
// is doing, rather than the state it is about to leave.
export function installStatus(i: TemplateInstall): string {
  const run = i.runs[0];
  if (i.busy && run) {
    return { install: "installing", update: "updating", uninstall: "uninstalling" }[run.kind];
  }
  return i.status;
}

// The Installed tab: every template on this instance, the release it is
// on, and the two things there are to do with one.
export function TemplateInstalls() {
  const router = useRouter();
  const [updating, setUpdating] = useState<TemplateInstall | null>(null);
  const [uninstalling, setUninstalling] = useState<TemplateInstall | null>(null);
  const installs = useQuery({
    queryKey: ["template-installs"],
    queryFn: () => api.get<TemplateInstall[]>("/template-installs"),
    // Only while something is changing: the list is otherwise still.
    refetchInterval: (q) => (q.state.data?.some((i) => i.busy) ? 3000 : false),
  });

  const columns: Column<TemplateInstall>[] = [
    {
      id: "template",
      header: "Template",
      width: 38,
      sortBy: (i) => i.repo,
      cell: (i) => (
        <div className="min-w-0">
          <div className="truncate font-mono text-sm">
            {i.owner}/{i.repo}
          </div>
          <div className="truncate font-mono text-xs text-subtle-foreground">
            {i.project}/{i.environment}
          </div>
        </div>
      ),
    },
    {
      id: "release",
      header: "Release",
      width: 28,
      sortBy: (i) => i.release,
      cell: (i) => (
        <span className="flex items-center gap-2">
          <span className="font-mono text-xs">{i.release}</span>
          {i.update_available && (
            <span className="border border-primary/40 px-1.5 py-0.5 font-mono text-[10px] text-primary">
              {i.update_available} available
            </span>
          )}
        </span>
      ),
    },
    {
      id: "status",
      header: "Status",
      width: 20,
      sortBy: (i) => installStatus(i),
      cell: (i) => <StatusBadge value={installStatus(i)} />,
    },
    {
      id: "actions",
      header: "",
      width: 14,
      align: "right",
      cell: (i) => (
        <RowActions>
          <RowAction
            icon={ArrowUpCircleIcon}
            label={i.update_available ? `Update to ${i.update_available}` : "Up to date"}
            disabled={!i.update_available || i.busy || i.status !== "installed"}
            onClick={() => setUpdating(i)}
          />
          <RowAction
            icon={Trash2Icon}
            label={`Uninstall ${i.repo}`}
            danger
            disabled={i.busy || i.status !== "installed"}
            onClick={() => setUninstalling(i)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <ErrorAlert error={installs.error ? message(installs.error) : null} />
      <DataTable
        columns={columns}
        rows={installs.data ?? null}
        rowKey={(i) => String(i.id)}
        loadingRows={3}
        empty="Nothing is installed yet. Install a template from the catalog."
        onRowClick={(i) => router.push(`/templates/installs/${i.id}`)}
      />
      {updating && (
        <TemplateUpdateDialog
          install={updating}
          open
          onOpenChange={(open) => !open && setUpdating(null)}
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
