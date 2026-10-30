"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeftIcon, RefreshCwIcon, Trash2Icon } from "lucide-react";
import { use, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { CopyField } from "@/components/copy-field";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import Link from "@/components/navigation-link";
import { SectionHeader } from "@/components/section-header";
import { StatusBadge } from "@/components/status-badge";
import StatusMark from "@/components/status-mark";
import { installStatus } from "@/components/template-installs";
import { TemplateUninstallDialog } from "@/components/template-uninstall-dialog";
import { TemplateUpdateDialog } from "@/components/template-update-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  api,
  type TemplateInstall,
  type TemplateResource,
  type TemplateRun,
  type TemplateUpdatePreview,
} from "@/lib/api";
import { message } from "@/lib/errors";
import { takeSecrets } from "@/lib/install-secrets";

// An installation: what it owns, the run changing it now, and its history.
// Polled while a run is going; a failed run has already been undone by the
// daemon, so what is left to say about one is why.
export default function TemplateInstallPage({ params }: PageProps<"/templates/installs/[id]">) {
  const { id } = use(params);
  const queryClient = useQueryClient();
  const [uninstalling, setUninstalling] = useState(false);
  const [updating, setUpdating] = useState(false);
  const install = useQuery({
    queryKey: ["template-installs", id],
    queryFn: () => api.get<TemplateInstall>(`/template-installs/${id}`),
    refetchInterval: (q) => (q.state.data && !q.state.data.busy ? false : 2000),
  });
  const actionable = install.data?.status === "installed" && !install.data.busy;
  // Compared again whenever the installation changes. A caller who may not
  // update, or a catalog that does not answer, is simply no button.
  const preview = useQuery({
    queryKey: ["template-installs", id, "update", install.data?.updated_at],
    queryFn: () => api.get<TemplateUpdatePreview>(`/template-installs/${id}/update`),
    enabled: actionable,
    retry: false,
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
  const needsName = i.status === "installed" && !i.installation_name;
  const run = i.runs[0];
  const gone = i.status === "failed" || i.status === "uninstalled";
  const owned = i.resources.filter((r) => r.kind !== "domain" && r.kind !== "attachment");

  return (
    <>
      {needsName && (
        <InstallationNameDialog
          install={i}
          onSaved={(updated) => {
            queryClient.setQueryData(["template-installs", id], updated);
            void queryClient.invalidateQueries({ queryKey: ["template-installs"] });
          }}
        />
      )}
      <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div className="min-w-0">
          <Link
            href="/templates?tab=installed"
            className="mb-4 inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeftIcon className="size-4" />
            Installed templates
          </Link>
          <h1 className="truncate text-2xl font-semibold tracking-tight">{i.name}</h1>
          <p className="mt-1 font-mono text-sm text-muted-foreground">
            {i.project}/{i.environment}
          </p>
        </div>
        <div className="flex items-center gap-3">
          {!i.busy && i.status !== "installed" && <StatusBadge value={installStatus(i)} />}
          {actionable && preview.data?.available && (
            <Button onClick={() => setUpdating(true)}>
              <RefreshCwIcon />
              Update
            </Button>
          )}
          {actionable && (
            <Button variant="outline" onClick={() => setUninstalling(true)}>
              <Trash2Icon />
              Uninstall
            </Button>
          )}
        </div>
      </div>

      <div className="space-y-6">
        {(run?.status === "running" || run?.status === "succeeded") && (
          <Card>
            <CardContent className="flex items-center gap-3">
              <div className="min-w-0">
                <StatusMark
                  status={run.status === "running" ? "running" : "done"}
                  label={
                    run.status === "running"
                      ? (run.step ?? "In progress")
                      : run.kind === "install"
                        ? "Installation completed"
                        : run.kind === "update"
                          ? "Update completed"
                          : "Uninstall completed"
                  }
                  size={20}
                  strike={false}
                />
              </div>
            </CardContent>
          </Card>
        )}
        {run?.status === "failed" && <ErrorAlert error={failure(run)} />}

        {Object.keys(secrets).length > 0 && (
          <Card>
            <CardContent className="space-y-4 [&>.dashboard-section-header]:mb-0">
              <SectionHeader title="Generated secrets" />
              <div className="grid gap-4 sm:grid-cols-2">
                {Object.entries(secrets).map(([key, value]) => (
                  <CopyField key={key} label={key} value={value} masked />
                ))}
              </div>
            </CardContent>
          </Card>
        )}

        {owned.length > 0 && (
          <Card>
            <CardContent className="space-y-4 [&>.dashboard-section-header]:mb-0">
              <SectionHeader title={gone ? "What it had" : "What it owns"} />
              <div className="divide-y divide-border">
                {owned.map((r) => (
                  <div key={`${r.kind}-${r.name}`} className="flex items-center gap-3 py-3">
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
              </div>
            </CardContent>
          </Card>
        )}

        {i.runs.length > 0 && (
          <Card>
            <CardContent className="space-y-4 [&>.dashboard-section-header]:mb-0">
              <SectionHeader title="History" />
              <div className="divide-y divide-border">
                {i.runs.map((r) => (
                  <div key={r.id} className="flex flex-wrap items-center gap-3 py-3">
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
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      {uninstalling && <TemplateUninstallDialog install={i} open onOpenChange={setUninstalling} />}
      {updating && preview.data && (
        <TemplateUpdateDialog
          install={i}
          preview={preview.data}
          open
          onOpenChange={setUpdating}
          onStarted={(handed) => {
            if (Object.keys(handed).length > 0) setSecrets(handed);
          }}
        />
      )}
    </>
  );
}

function InstallationNameDialog({
  install,
  onSaved,
}: {
  install: TemplateInstall;
  onSaved: (install: TemplateInstall) => void;
}) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const updated = await api.patch<TemplateInstall>(`/template-installs/${install.id}`, {
        installation_name: name.trim(),
      });
      onSaved(updated);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open onOpenChange={() => {}}>
      <DialogContent showCloseButton={false}>
        <form onSubmit={save} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Name this installation</DialogTitle>
            <DialogDescription>
              Choose a name to identify it in Installed templates. Its existing apps and data keep
              their current names.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="installation-name" className="text-xs text-muted-foreground">
              Installation Name
            </Label>
            <Input
              id="installation-name"
              autoFocus
              spellCheck={false}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={install.repo}
            />
            <p className="text-xs text-muted-foreground">
              Use lowercase letters, numbers and dashes.
            </p>
          </div>
          <ErrorAlert error={error} />
          <DialogFooter>
            <ActionButton type="submit" busy={busy} disabled={!name.trim()}>
              Save name
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
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
