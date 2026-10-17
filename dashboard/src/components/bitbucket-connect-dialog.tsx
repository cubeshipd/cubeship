"use client";

import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api } from "@/lib/api";
import { message } from "@/lib/errors";

export function BitbucketConnectDialog({ onConnected }: { onConnected: () => void }) {
  const [open, setOpen] = useState(false);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function connect(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post("/bitbucket", { api_token: token.trim() });
      setToken("");
      setOpen(false);
      onConnected();
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          setToken("");
          setError(null);
        }
      }}
    >
      <Button size="sm" onClick={() => setOpen(true)}>
        Connect
      </Button>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={connect} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Connect Bitbucket</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            Create a scoped{" "}
            <a
              className="text-foreground underline underline-offset-4"
              href="https://id.atlassian.com/manage-profile/security/api-tokens"
              target="_blank"
              rel="noreferrer noopener"
            >
              Bitbucket API token
            </a>{" "}
            with user, workspace and repository read permissions. No OAuth consumer is needed.
          </p>
          <ErrorAlert error={error} />
          <TextField
            label="API token"
            type="password"
            autoComplete="off"
            value={token}
            onChange={(event) => setToken(event.target.value)}
            required
            spellCheck={false}
            hint="Scopes: read:user:bitbucket, read:workspace:bitbucket, read:repository:bitbucket"
          />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy}>
              Connect
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
