"use client";

import { EyeIcon, EyeOffIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CopyField } from "@/components/copy-field";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { SectionHeader } from "@/components/section-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ApiError, type App, api, BUILDING_SOURCES, type DeployHook } from "@/lib/api";
import { message } from "@/lib/errors";

// The URL that deploys this app when something posts to it — for a CI
// that pushed somewhere Cubeship cannot see.
//
// The token is hidden until asked for, and copied either way: a URL on
// screen is a URL in somebody's screenshot. Seeing it takes what
// deploying the app takes, so a member looking at an app that builds
// sees no card at all, not an error.
export function AppDeployHook({ app }: { app: App }) {
  const [hook, setHook] = useState<DeployHook | null>(null);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [revealed, setRevealed] = useState(false);
  const [rotating, setRotating] = useState(false);
  const path = `/apps/${app.reference}/deploy-hook`;

  useEffect(() => {
    setHook(null);
    setForbidden(false);
    setError(null);
    api
      .get<DeployHook>(path)
      .then(setHook)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 403) {
          setForbidden(true);
          return;
        }
        setError(message(e));
      });
  }, [path]);

  const builds = BUILDING_SOURCES.includes(app.source);
  // Until the instance has a domain there is no public address, and a
  // guessed one would be a link that does not work.
  const url = hook?.url ?? null;
  const shown = hook && url && !revealed ? url.replace(hook.token, "•".repeat(16)) : url;
  const param = builds ? "ref=main" : "tag=1.2.3";

  if (forbidden) return null;

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
          <SectionHeader
            title="Deploy webhook"
            sub="POST to this URL to deploy the app. No API key: the token in it is the authorization, so keep it with your CI's other secrets."
            actions={
              hook &&
              url && (
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => setRevealed(!revealed)}
                  aria-label={revealed ? "Hide the token" : "Reveal the token"}
                >
                  {revealed ? <EyeOffIcon /> : <EyeIcon />}
                  {revealed ? "Hide" : "Reveal"}
                </Button>
              )
            }
          />
          <ErrorAlert error={error} />
          {hook && !url && (
            <Notice>
              This instance has no domain yet, so the webhook has no address. Set the domain in
              Settings and it appears here.
            </Notice>
          )}
          {hook && url && shown && (
            <>
              <CopyField
                label="Webhook URL"
                value={shown}
                copyValue={url}
                className="font-mono"
                hint="Copied whole, token included, even while it is hidden."
              />
              <div className="space-y-2 text-sm text-muted-foreground">
                <ul className="list-disc space-y-1 pl-5">
                  {builds ? (
                    <li>
                      <code className="text-foreground">ref</code> — the branch, tag or commit to
                      build, for this deploy only.
                    </li>
                  ) : (
                    <li>
                      <code className="text-foreground">tag</code> — the image tag to run.{" "}
                      {app.autodeploy
                        ? "Deployed without being saved, so pushes keep deploying the app."
                        : "Saved on the app, so the next deploy from anywhere stays on it."}
                    </li>
                  )}
                  <li>
                    <code className="text-foreground">wait=true</code> — answer once the deploy has
                    finished, instead of as soon as it starts.
                  </li>
                </ul>
                <pre className="overflow-x-auto rounded-lg border border-border bg-secondary/40 p-3 font-mono text-xs whitespace-pre-wrap break-all">
                  curl -X POST "{shown}?{param}"
                </pre>
              </div>
              <ActionButton variant="outline" onClick={() => setRotating(true)}>
                Regenerate
              </ActionButton>
            </>
          )}
        </CardContent>
      </Card>
      <ConfirmDialog
        open={rotating}
        onOpenChange={setRotating}
        title="Regenerate the deploy webhook?"
        confirmLabel="Regenerate"
        description="The current URL stops working at once. Every CI that uses it has to be given the new one."
        onConfirm={async () => {
          setHook(await api.post<DeployHook>(`${path}/rotate`));
          setRevealed(false);
        }}
      />
    </>
  );
}
