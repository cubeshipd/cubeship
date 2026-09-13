"use client";

import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { TextAreaField, TextField } from "@/components/text-field";
import { Card, CardContent } from "@/components/ui/card";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

// Registering the instance as a GitHub App is what makes building a
// private repository and deploying on a push possible at all. One App
// per instance, registered by whoever runs the VPS; organizations then
// install it on their own accounts.
//
// The credentials are write-only. The daemon reports whether they are
// there and never what they are, so this shows a state rather than the
// values — an endpoint that handed a private key back would turn every
// read of the configuration into a way out for it.
export function GitHubAppCard({
  settings,
  onSaved,
}: {
  settings: Settings;
  onSaved: (s: Settings) => void;
}) {
  // Closed by default either way: an instance with no App is offered
  // the one-click flow first, and the four fields are the escape hatch
  // under it.
  const [open, setOpen] = useState(false);
  const [appId, setAppId] = useState("");
  const [slug, setSlug] = useState(settings.github_app_slug ?? "");
  const [key, setKey] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The address this dashboard is being read at is the address GitHub
  // has to reach back on — by IP before there is a domain, by name
  // after. Deriving it beats asking someone to work it out.
  const origin = typeof window === "undefined" ? "" : window.location.origin;
  const webhookURL = `${origin}/hooks/github`;
  const setupURL = `${origin}/github/connected`;

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onSaved(
        await api.put<Settings>("/settings", {
          github_app_id: appId.trim(),
          github_app_slug: slug.trim(),
          github_private_key: key,
          github_webhook_secret: secret,
        }),
      );
      setKey("");
      setSecret("");
      setAppId("");
      setOpen(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Card className="mb-4">
      <CardContent className="space-y-4">
        <ErrorAlert error={error} />

        {!open ? (
          <button
            type="button"
            onClick={() => setOpen(true)}
            className="text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
          >
            Already have a GitHub App? Enter its credentials instead.
          </button>
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              For an App you already have. Point its <strong>Webhook URL</strong> at{" "}
              <code className="text-xs">{webhookURL}</code>, its <strong>Setup URL</strong> and one{" "}
              <strong>Callback URL</strong> at <code className="text-xs">{setupURL}</code>, give it{" "}
              <code className="text-xs">Contents</code> and{" "}
              <code className="text-xs">Metadata</code> read-only, subscribe it to{" "}
              <code className="text-xs">Push</code>, and make it public with{" "}
              <strong>Request user authorization on install</strong> on — without that last pair it
              can only ever be installed on the account that owns it, and an installation cannot be
              proved to be yours.
            </p>

            <form onSubmit={save} className="space-y-4">
              <TextField
                label="App ID"
                hint="The number on the App's settings page."
                value={appId}
                onChange={(e) => setAppId(e.target.value)}
                placeholder="123456"
                spellCheck={false}
              />
              <TextField
                label="App slug"
                hint="From its URL: github.com/apps/<slug>. It is how the install page is addressed."
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                placeholder="cubeship-acme"
                spellCheck={false}
              />
              <TextAreaField
                label="Private key"
                hint="The .pem GitHub downloads when you generate one. Stored, never shown again."
                value={key}
                onChange={(e) => setKey(e.target.value)}
                placeholder="-----BEGIN RSA PRIVATE KEY-----"
                rows={4}
                spellCheck={false}
              />
              <TextField
                label="Webhook secret"
                type="password"
                hint="The same string you put in the App's webhook secret. Without it a delivery cannot be trusted, and pushes will not deploy."
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                spellCheck={false}
              />
              <div className="flex items-center gap-3">
                <ActionButton type="submit" busy={busy}>
                  {busy ? "Saving" : "Save"}
                </ActionButton>
                <ActionButton type="button" variant="ghost" onClick={() => setOpen(false)}>
                  Cancel
                </ActionButton>
              </div>
            </form>
          </>
        )}
      </CardContent>
    </Card>
  );
}
