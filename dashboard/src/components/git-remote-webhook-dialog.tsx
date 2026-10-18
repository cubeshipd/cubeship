"use client";

import { useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { api, type GitRemoteConnection } from "@/lib/api";
import { message } from "@/lib/errors";

export function GitRemoteWebhookDialog({ connection }: { connection: GitRemoteConnection }) {
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState("");
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    api
      .get<{ secret: string }>(`/gitremotes/${connection.id}/webhook`)
      .then((result) => setSecret(result.secret))
      .catch((err) => setError(message(err)));
  }, [open, connection.id]);
  const origin = typeof window === "undefined" ? "" : window.location.origin;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          setSecret("");
          setError(null);
        }
      }}
    >
      <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
        Webhook
      </Button>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Deploy on push · {connection.account}</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">
          Add a repository push webhook in {connection.provider === "gitlab" ? "GitLab" : "Gitea"}.
          Use JSON as the content type and this URL and secret.
        </p>
        <ErrorAlert error={error} />
        <TextField
          label="Webhook URL"
          value={`${origin}/hooks/gitremotes/${connection.id}`}
          readOnly
        />
        <TextField label="Secret" value={secret} readOnly placeholder="Loading…" />
      </DialogContent>
    </Dialog>
  );
}
