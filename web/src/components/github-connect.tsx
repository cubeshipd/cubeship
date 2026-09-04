"use client";

import { PlugIcon } from "lucide-react";
import { CreateGitHubApp } from "@/components/github-app-manifest";
import { Button } from "@/components/ui/button";
import type { Settings } from "@/lib/api";

// One button, whatever state the instance is in.
//
// Connecting to GitHub is two things underneath — this instance has to
// exist as a GitHub App, and that App has to be installed on an account
// — and nobody sets out to create a GitHub App. They set out to connect
// their code. So the button says Connect either way and the flow
// continues on its own: registering redirects straight into installing,
// and installing redirects back here connected.
//
// The App cannot be one Cubeship publishes centrally, the way Vercel or
// Railway do. Every instance is a different address, and an App carries
// the webhook and callback URLs it delivers to — an App owned by us
// would deliver this instance's pushes to somewhere else entirely. The
// manifest flow is what makes an instance-owned App a click rather than
// a form.
export function ConnectGitHub({
  settings,
  instanceName,
  size,
}: {
  settings: Settings | undefined;
  instanceName: string;
  size?: "sm" | "xs";
}) {
  if (!settings) return null;

  // An App that cannot reach an organization is not an App to install
  // again. It was registered private — a private GitHub App only ever
  // installs on the account that owns it — and neither that nor the
  // missing OAuth can be changed after the fact. So the button makes a
  // new one, which is the only thing that helps.
  const usable = settings.github_oauth_ready ?? false;

  // Already a usable App: skip straight to installing it.
  if (settings.github_connected && settings.github_app_slug && usable) {
    return (
      <Button
        size={size}
        nativeButton={false}
        render={
          <a
            href={`https://github.com/apps/${settings.github_app_slug}/installations/new`}
            target="_blank"
            rel="noreferrer noopener"
          >
            <PlugIcon />
            Connect GitHub
          </a>
        }
      />
    );
  }

  return (
    <CreateGitHubApp instanceName={instanceName} label="Connect GitHub" note={false} size={size} />
  );
}
