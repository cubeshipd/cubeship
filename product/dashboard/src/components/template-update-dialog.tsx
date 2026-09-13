"use client";

import { useQuery } from "@tanstack/react-query";
import { cn } from "cn";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { secretsKey } from "@/app/(dashboard)/templates/[owner]/[repo]/page";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { Button } from "@/components/ui/button";
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
  type TemplateChange,
  type TemplateInstall,
  type TemplateRunStarted,
  type TemplateUpdatePreview,
} from "@/lib/api";
import { message } from "@/lib/errors";

const ACTIONS: Record<TemplateChange["action"], { label: string; tone: string }> = {
  create: { label: "Create", tone: "text-success" },
  change: { label: "Change", tone: "text-warning" },
  keep: { label: "Keep", tone: "text-muted-foreground" },
};

// Updating is always shown before it is done: what the release creates,
// what it changes on apps somebody may have edited by hand, and what it
// leaves in place. Nothing is deleted either way.
export function TemplateUpdateDialog({
  install,
  open,
  onOpenChange,
}: {
  install: TemplateInstall;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const router = useRouter();
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const preview = useQuery({
    queryKey: ["template-update", install.id, install.update_available],
    queryFn: () => api.get<TemplateUpdatePreview>(`/template-installs/${install.id}/update`),
    enabled: open,
  });

  const unanswered = (preview.data?.inputs ?? []).some((i) => !answers[i.key]?.trim());

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!preview.data) return;
    setBusy(true);
    setError(null);
    try {
      const started = await api.post<TemplateRunStarted>(
        `/template-installs/${install.id}/update`,
        {
          release: preview.data.to,
          inputs: answers,
        },
      );
      if (started.secrets && Object.keys(started.secrets).length > 0) {
        sessionStorage.setItem(secretsKey(install.id), JSON.stringify(started.secrets));
      }
      onOpenChange(false);
      router.push(`/templates/installs/${install.id}`);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={submit} className="space-y-4">
          <DialogHeader>
            <DialogTitle>
              Update {install.repo}
              {preview.data && (
                <span className="ml-2 font-mono text-sm text-muted-foreground">
                  {preview.data.from} → {preview.data.to}
                </span>
              )}
            </DialogTitle>
            <DialogDescription>
              Nothing is deleted. If a step fails, the installation is put back as it is now.
            </DialogDescription>
          </DialogHeader>

          <ErrorAlert error={preview.error ? message(preview.error) : error} />
          {preview.isLoading && <LoadingList rows={3} />}

          {preview.data && (
            <div className="max-h-72 divide-y divide-border overflow-y-auto border border-border">
              {preview.data.changes.length === 0 && (
                <p className="px-3 py-4 text-sm text-muted-foreground">
                  The release changes nothing this installation has.
                </p>
              )}
              {preview.data.changes.map((c) => (
                <div key={`${c.action}-${c.kind}-${c.name}-${c.detail}`} className="px-3 py-2">
                  <div className="flex items-baseline gap-2 text-sm">
                    <span className={cn("w-14 shrink-0 text-xs uppercase", ACTIONS[c.action].tone)}>
                      {ACTIONS[c.action].label}
                    </span>
                    <span className="shrink-0 text-xs text-muted-foreground">{c.kind}</span>
                    <span className="min-w-0 truncate font-mono text-xs">{c.name}</span>
                  </div>
                  {c.detail && (
                    <p className="mt-0.5 pl-16 text-xs text-subtle-foreground">{c.detail}</p>
                  )}
                </div>
              ))}
            </div>
          )}

          {preview.data && preview.data.inputs.length > 0 && (
            <div className="space-y-3">
              <p className="text-xs text-muted-foreground">The release asks something new.</p>
              {preview.data.inputs.map((i) => (
                <div key={i.key} className="space-y-2">
                  <Label htmlFor={`update-${i.key}`} className="text-xs text-muted-foreground">
                    {i.label}
                  </Label>
                  <Input
                    id={`update-${i.key}`}
                    type={i.type === "secret" ? "password" : "text"}
                    value={answers[i.key] ?? ""}
                    onChange={(e) => setAnswers({ ...answers, [i.key]: e.target.value })}
                  />
                  {i.help && <p className="text-xs text-subtle-foreground">{i.help}</p>}
                </div>
              ))}
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!preview.data || unanswered || busy}>
              {busy ? "Starting…" : `Update to ${preview.data?.to ?? "…"}`}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
