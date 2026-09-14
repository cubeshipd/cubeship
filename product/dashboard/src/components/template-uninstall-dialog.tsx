"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, type TemplateInstall, type TemplateRunStarted } from "@/lib/api";
import { message } from "@/lib/errors";

// Uninstalling keeps the data unless somebody says otherwise twice: once by
// unticking the box, and again by typing the installation's name, because
// that half cannot be undone.
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
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const data = install.resources.filter((r) => r.kind === "database" || r.kind === "store");
  const deletingData = !keepData && data.length > 0;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
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
        <form onSubmit={submit} className="space-y-4">
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
            <>
              <Notice tone="warning">
                Their data is deleted permanently, and there is no backup of it here.
              </Notice>
              <div className="space-y-2">
                <Label htmlFor="uninstall-confirm" className="text-xs text-muted-foreground">
                  Type <span className="font-mono text-foreground">{install.repo}</span> to confirm
                </Label>
                <Input
                  id="uninstall-confirm"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  autoComplete="off"
                />
              </div>
            </>
          )}

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant="destructive"
              disabled={busy || (deletingData && typed !== install.repo)}
            >
              {busy ? "Starting…" : "Uninstall"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
