"use client";

import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { TextField } from "@/components/text-field";
import { Card, CardContent } from "@/components/ui/card";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

export function BitbucketWebhookCard({ onSaved }: { onSaved: (settings: Settings) => void }) {
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const origin = typeof window === "undefined" ? "" : window.location.origin;

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onSaved(await api.put<Settings>("/settings", { bitbucket_webhook_secret: secret }));
      setSecret("");
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="mt-4">
      <CardContent>
        <form onSubmit={save} className="space-y-4">
          <div>
            <h2 className="text-sm font-medium">Bitbucket push deployments</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              To deploy on push, add a repository push webhook in Bitbucket pointing to{" "}
              <code>{origin}/hooks/bitbucket</code> and use the same signing secret here.
            </p>
          </div>
          <ErrorAlert error={error} />
          <TextField
            label="Webhook signing secret"
            type="password"
            value={secret}
            onChange={(event) => setSecret(event.target.value)}
            required
            autoComplete="off"
          />
          <ActionButton type="submit" busy={busy}>
            Save webhook secret
          </ActionButton>
        </form>
      </CardContent>
    </Card>
  );
}
