"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowUpCircleIcon, LoaderCircleIcon, Trash2Icon } from "lucide-react";
import { use, useEffect, useState } from "react";
import { CopyField } from "@/components/copy-field";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal } from "@/components/header-rail";
import { LoadingList } from "@/components/loading";
import Link from "@/components/navigation-link";
import { Notice } from "@/components/notice";
import { SectionHeader } from "@/components/section-header";
import { StatusBadge } from "@/components/status-badge";
import { installStatus } from "@/components/template-installs";
import { TemplateUninstallDialog } from "@/components/template-uninstall-dialog";
import { TemplateUpdateDialog } from "@/components/template-update-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { api, type TemplateInstall, type TemplateResource, type TemplateRun } from "@/lib/api";
import { message } from "@/lib/errors";
import { takeSecrets } from "@/lib/install-secrets";

// An installation: what it owns, the run changing it now, and its history.
// Polled while a run is going; a failed run has already been undone by the
// daemon, so what is left to say about one is why.
export default function TemplateInstallPage({ params }: PageProps<"/templates/installs/[id]">) {
  const { id } = use(params);
  const [updating, setUpdating] = useState(false);
  const [uninstalling, setUninstalling] = useState(false);
  const install = useQuery({
    queryKey: ["template-installs", id],
    queryFn: () => api.get<TemplateInstall>(`/template-installs/${id}`),
    refetchInterval: (q) => (q.state.data && !q.state.data.busy ? false : 2000),
  });

  // Read once and forgotten: this page is the one place they are shown.
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  useEffect(() => {
    const handed = takeSecrets(Number(id));
    if (Object.keys(handed).length > 0) setSecrets(handed);
  }, [id]);

  if (install.error) return <ErrorAlert error={message(install.error)} />;
  if (!install.data) return <LoadingList rows={4} />;
  const i = install.data;
  const run = i.runs[0];
  const actionable = i.status === "installed" && !i.busy;
  const gone = i.status === "failed" || i.status === "uninstalled";
  const owned = i.resources.filter((r) => r.kind !== "domain" && r.kind !== "attachment");

  return (
    <>
      <RailPortal>
        {actionable && (
          <>
            {i.update_available && (
              <Button onClick={() => setUpdating(true)}>
                <ArrowUpCircleIcon />
                Update to {i.update_available}
              </Button>
            )}
            <Button variant="outline" onClick={() => setUninstalling(true)}>
              <Trash2Icon />
              Uninstall
            </Button>
          </>
        )}
      </RailPortal>

      <div className="mb-6 flex items-center justify-between gap-4">
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold">
            {i.owner}/{i.repo}{" "}
            <span className="font-mono text-sm text-muted-foreground">{i.release}</span>
          </h1>
          <p className="font-mono text-xs text-subtle-foreground">
            in {i.project}/{i.environment}
          </p>
        </div>
        <StatusBadge value={installStatus(i)} />
      </div>

      {run?.status === "running" && (
        <p className="mb-6 flex items-center gap-2 text-sm text-muted-foreground">
          <LoaderCircleIcon className="size-4 animate-spin text-primary" aria-hidden />
          {run.step}
        </p>
      )}
      {run?.status === "failed" && <ErrorAlert error={failure(run)} />}

      {Object.keys(secrets).length > 0 && (
        <>
          <SectionHeader title="Generated secrets" />
          <Notice tone="warning" className="mb-3">
            Shown this once. Keep a copy — they are in the apps' variables, and nowhere else.
          </Notice>
          <Card>
            <CardContent className="grid gap-4 sm:grid-cols-2">
              {Object.entries(secrets).map(([key, value]) => (
                <CopyField key={key} label={key} value={value} masked />
              ))}
            </CardContent>
          </Card>
        </>
      )}

      {owned.length > 0 && (
        <>
          <SectionHeader title={gone ? "What it had" : "What it owns"} />
          <Card className="gap-0 divide-y divide-border py-0">
            {owned.map((r) => (
              <div key={`${r.kind}-${r.name}`} className="flex items-center gap-3 px-4 py-2.5">
                <span className="w-24 shrink-0 text-xs uppercase tracking-[0.14em] text-muted-foreground">
                  {r.kind}
                </span>
                {gone ? (
                  <span className="font-mono text-sm text-muted-foreground line-through">
                    {r.name}
                  </span>
                ) : (
                  <Link href={hrefFor(r)} className="font-mono text-sm hover:text-primary">
                    {r.name}
                  </Link>
                )}
              </div>
            ))}
          </Card>
        </>
      )}

      {i.runs.length > 0 && (
        <>
          <SectionHeader title="History" />
          <Card className="gap-0 divide-y divide-border py-0">
            {i.runs.map((r) => (
              <div key={r.id} className="flex items-center gap-3 px-4 py-2.5">
                <span className="w-24 shrink-0 text-xs uppercase tracking-[0.14em] text-muted-foreground">
                  {r.kind}
                </span>
                <span className="min-w-0 flex-1 truncate font-mono text-xs">{releases(r)}</span>
                <span className="shrink-0 text-xs text-subtle-foreground">
                  {new Date(r.created_at).toLocaleString()}
                </span>
                <StatusBadge value={r.status} />
              </div>
            ))}
          </Card>
        </>
      )}

      {updating && <TemplateUpdateDialog install={i} open onOpenChange={setUpdating} />}
      {uninstalling && <TemplateUninstallDialog install={i} open onOpenChange={setUninstalling} />}
    </>
  );
}

function failure(run: TemplateRun) {
  switch (run.kind) {
    case "install":
      return `${run.error} Everything the install created was deleted.`;
    case "update":
      return `${run.error} The installation was put back on ${run.from_release}.`;
    case "uninstall":
      return `${run.error} The installation is still installed.`;
  }
}

function releases(run: TemplateRun) {
  if (run.kind === "update") return `${run.from_release} → ${run.to_release}`;
  if (run.kind === "uninstall") return run.keep_data ? "kept the data" : "deleted the data";
  return run.to_release ?? "";
}

function hrefFor(r: TemplateResource) {
  switch (r.kind) {
    case "database":
      return `/databases/${r.name}`;
    case "store":
      return `/storage/${r.name}`;
    default:
      return `/projects/${r.name}`;
  }
}
