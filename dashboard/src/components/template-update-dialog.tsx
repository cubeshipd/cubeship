"use client";

import { useQueryClient } from "@tanstack/react-query";
import { cn } from "cn";
import { useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
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
  remove: { label: "Remove", tone: "text-destructive" },
  keep: { label: "Keep", tone: "text-muted-foreground" },
};

// An update is always shown before it is done: what the template now
// creates, what it changes on apps somebody may have edited by hand, the
// domains it moves, and what it leaves in place.
export function TemplateUpdateDialog({
  install,
  preview,
  open,
  onOpenChange,
  onStarted,
}: {
  install: TemplateInstall;
  preview: TemplateUpdatePreview;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onStarted: (secrets: Record<string, string>) => void;
}) {
  const queryClient = useQueryClient();
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const unanswered = preview.inputs.some((i) => !answers[i.key]?.trim());

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const started = await api.post<TemplateRunStarted>(
        `/template-installs/${install.id}/update`,
        {
          inputs: answers,
        },
      );
      onStarted(started.secrets ?? {});
      // The installation's copy says nothing is running, so it would never
      // poll for the run just started.
      await queryClient.invalidateQueries({ queryKey: ["template-installs"] });
      onOpenChange(false);
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
            <DialogTitle>Update {install.name}</DialogTitle>
            <DialogDescription>
              Brings it in line with the template the catalog lists now. Only domains the template
              no longer declares are removed. If a step fails, the installation is put back as it is
              now.
            </DialogDescription>
          </DialogHeader>

          <ErrorAlert error={error} />

          <div className="max-h-72 divide-y divide-border overflow-y-auto border border-border">
            {preview.changes.length === 0 && (
              <p className="px-3 py-4 text-sm text-muted-foreground">
                The template changed nothing this installation has.
              </p>
            )}
            {preview.changes.map((c) => (
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

          {preview.inputs.length > 0 && (
            <div className="space-y-3">
              <p className="text-xs text-muted-foreground">The template asks something new.</p>
              {preview.inputs.map((i) => (
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
            <Button type="submit" disabled={unanswered || busy}>
              {busy ? "Starting…" : "Update"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
