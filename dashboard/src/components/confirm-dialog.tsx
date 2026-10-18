"use client";

import { type ReactNode, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import HoldButton from "@/components/hold-button";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { message } from "@/lib/errors";

// Shared confirmation state and error handling. Deletions can require a
// hold; non-delete operations that need a target word keep that prompt.
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmWord,
  confirmLabel = "Delete",
  holdToConfirm = false,
  onConfirm,
  children,
}: {
  // Choices that belong to the confirmation, above the typed name.
  children?: ReactNode;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  title: string;
  description: ReactNode;
  // What has to be typed back. Omit it for a confirmation that only
  // needs a deliberate second click.
  confirmWord?: string;
  confirmLabel?: string;
  holdToConfirm?: boolean;
  onConfirm: () => Promise<unknown>;
}) {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function confirm() {
    setBusy(true);
    setError(null);
    try {
      await onConfirm();
      setTyped("");
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    await confirm();
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form className="min-w-0" onSubmit={submit}>
          <DialogHeader>
            <DialogTitle className="text-destructive">{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-5">
            <ErrorAlert error={error} />
            {children}
            {confirmWord && !holdToConfirm && (
              <TextField
                label={`Type ${confirmWord} to confirm`}
                spellCheck={false}
                autoFocus
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
              />
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            {holdToConfirm ? (
              <HoldButton
                className="w-full sm:w-auto"
                backgroundColor="var(--muted)"
                fillColor="var(--destructive)"
                textColor="var(--foreground)"
                fillTextColor="var(--destructive-foreground)"
                size="sm"
                radius={8}
                disabled={busy}
                doneLabel="Working…"
                onHold={() => void confirm()}
              >
                Hold to {confirmLabel.toLowerCase()}
              </HoldButton>
            ) : (
              <ActionButton
                type="submit"
                busy={busy}
                variant="destructive"
                disabled={confirmWord ? typed !== confirmWord : false}
              >
                {confirmLabel}
              </ActionButton>
            )}
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
