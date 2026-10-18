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

export function GitRemoteConnectDialog({
  provider,
  onConnected,
  connected = false,
}: {
  provider: "gitlab" | "gitea";
  onConnected: () => void;
  connected?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [baseURL, setBaseURL] = useState(provider === "gitlab" ? "https://gitlab.com" : "");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const name = provider === "gitlab" ? "GitLab" : "Gitea";

  async function connect(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post("/gitremotes", {
        provider,
        base_url: baseURL.trim(),
        api_token: token.trim(),
      });
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
      <Button size="sm" variant={connected ? "outline" : "default"} onClick={() => setOpen(true)}>
        {connected ? "Add instance" : "Connect"}
      </Button>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={connect} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Connect {name}</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            {provider === "gitlab"
              ? "Use GitLab.com or the HTTPS URL of your self-hosted GitLab. The token needs read_api and read_repository scopes."
              : "Enter the HTTPS URL of your Gitea instance. The token needs read:user, read:organization and read:repository scopes."}
          </p>
          <ErrorAlert error={error} />
          <TextField
            label="Instance URL"
            type="url"
            value={baseURL}
            onChange={(event) => setBaseURL(event.target.value)}
            placeholder={provider === "gitlab" ? "https://gitlab.com" : "https://git.example.com"}
            required
            spellCheck={false}
          />
          <TextField
            label="Personal access token"
            type="password"
            autoComplete="off"
            value={token}
            onChange={(event) => setToken(event.target.value)}
            required
            spellCheck={false}
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
