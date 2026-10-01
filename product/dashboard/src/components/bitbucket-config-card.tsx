"use client";

import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { TextField } from "@/components/text-field";
import { Card, CardContent } from "@/components/ui/card";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

export function BitbucketConfigCard({
  settings,
  onSaved,
}: {
  settings: Settings;
  onSaved: (s: Settings) => void;
}) {
  const configured = settings.bitbucket_configured === true;
  const [open, setOpen] = useState(!configured);
  const [clientID, setClientID] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [webhookSecret, setWebhookSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const origin = typeof window === "undefined" ? "" : window.location.origin;

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const values: Record<string, string> = {};
    if (clientID.trim()) values.bitbucket_client_id = clientID.trim();
    if (clientSecret) values.bitbucket_client_secret = clientSecret;
    if (webhookSecret) values.bitbucket_webhook_secret = webhookSecret;
    try {
      onSaved(await api.put<Settings>("/settings", values));
      setClientID("");
      setClientSecret("");
      setWebhookSecret("");
      setOpen(false);
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="mt-4">
      <CardContent className="space-y-4">
        <ErrorAlert error={error} />
        {!open ? (
          <button
            type="button"
            onClick={() => setOpen(true)}
            className="text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
          >
            Update Bitbucket credentials
          </button>
        ) : (
          <form onSubmit={save} className="space-y-4">
            <p className="text-sm text-muted-foreground">
              Create a Bitbucket Cloud OAuth consumer, set its callback URL to{" "}
              <code className="text-xs">{origin}/bitbucket/connected</code>, and set its webhook
              signing secret to the same value used by{" "}
              <code className="text-xs">{origin}/hooks/bitbucket</code>.
            </p>
            <TextField
              label="OAuth consumer key"
              value={clientID}
              onChange={(e) => setClientID(e.target.value)}
              required={!configured}
              spellCheck={false}
            />
            <TextField
              label="OAuth consumer secret"
              type="password"
              value={clientSecret}
              onChange={(e) => setClientSecret(e.target.value)}
              required={!configured}
            />
            <TextField
              label="Webhook secret"
              type="password"
              value={webhookSecret}
              onChange={(e) => setWebhookSecret(e.target.value)}
              hint="Optional for connecting an account; required for deploys triggered by pushes."
            />
            <div className="flex items-center gap-3">
              <ActionButton type="submit" busy={busy}>
                {busy ? "Saving" : "Save"}
              </ActionButton>
              {configured && (
                <ActionButton type="button" variant="ghost" onClick={() => setOpen(false)}>
                  Cancel
                </ActionButton>
              )}
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
