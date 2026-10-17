"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import HoldButton from "@/components/hold-button";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { api, type TemplateInstall, type TemplateRunStarted } from "@/lib/api";
import { message } from "@/lib/errors";

// Data stays by default; a hold confirms uninstalling, including when data is removed.
export function TemplateUninstallDialog({
  install,
  open,
  onOpenChange,
}: {
  install: TemplateInstall;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [keepData, setKeepData] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const data = install.resources.filter((r) => r.kind === "database" || r.kind === "store");
  const deletingData = !keepData && data.length > 0;

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      await api.post<TemplateRunStarted>(`/template-installs/${install.id}/uninstall`, {
        keep_data: keepData,
      });
      // Before the push, for the reason updating does it: from the
      // installation's own page the push goes nowhere, and polling would
      // never start for the run just begun.
      await queryClient.invalidateQueries({ queryKey: ["template-installs"] });
      onOpenChange(false);
      router.push(`/templates/installs/${install.id}`);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <div className="space-y-4">
          <DialogHeader>
            <DialogTitle>Uninstall {install.repo}</DialogTitle>
            <DialogDescription>
              Deletes the apps it created, and {install.project}/{install.environment} too if the
              install created it and nothing else is left in it.
            </DialogDescription>
          </DialogHeader>
          <ErrorAlert error={error} />

          {data.length > 0 && (
            <div className="space-y-2">
              <Label className="flex items-center gap-2 text-sm">
                <Checkbox checked={keepData} onCheckedChange={(v) => setKeepData(Boolean(v))} />
                Keep its databases and object stores
              </Label>
              <p className="pl-6 font-mono text-xs text-subtle-foreground">
                {data.map((r) => r.name).join(", ")}
              </p>
            </div>
          )}

          {deletingData && (
            <Notice tone="warning">
              Their data is deleted permanently, and there is no backup of it here.
            </Notice>
          )}

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <HoldButton
              className="w-full sm:w-auto"
              backgroundColor="var(--muted)"
              fillColor="var(--destructive)"
              textColor="var(--foreground)"
              fillTextColor="var(--destructive-foreground)"
              size="sm"
              radius={8}
              disabled={busy}
              doneLabel="Starting…"
              onHold={() => void submit()}
            >
              Hold to uninstall
            </HoldButton>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  );
}
