"use client";

import { CheckIcon, ExternalLinkIcon } from "lucide-react";
import type { ComponentType } from "react";
import { CreateGitHubApp } from "@/components/github-app-manifest";
import { Label } from "@/components/ui/label";

// The providers Cubeship can build from. GitHub is the only one today,
// and the shape is the point: adding GitLab is a row here rather than a
// second path through everything below.
export type Provider = {
  id: string;
  name: string;
  icon: ComponentType<{ className?: string }>;
  // connected is what turns the button into a state.
  connected: boolean;
  // href is where connecting sends you. Empty means the instance is not
  // registered with this provider yet, and the button creates the
  // registration instead of sending someone off to find it.
  href: string;
};

export function GitProviders({
  providers,
  canRegister,
  returnTo,
}: {
  providers: Provider[];
  // Registering the instance with a provider is the operator's, not an
  // organization admin's. Someone who cannot do it is told, rather than
  // shown a button that fails at the last step.
  canRegister: boolean;
  returnTo?: string;
}) {
  return (
    <div className="space-y-1.5">
      <Label>Provider</Label>
      <div className="flex flex-wrap gap-2">
        {providers.map((p) =>
          p.connected ? (
            <span
              key={p.id}
              className="inline-flex h-9 items-center gap-2 border border-border bg-secondary px-3 text-sm"
            >
              <p.icon className="size-4 shrink-0" />
              {p.name}
              <CheckIcon className="size-3.5 shrink-0 text-primary" />
            </span>
          ) : p.href ? (
            // A real anchor: Base UI's Button wants a native button in
            // `render`, and this opens a page on GitHub.
            <a
              key={p.id}
              href={p.href}
              target="_blank"
              rel="noreferrer"
              className="inline-flex h-9 items-center gap-2 border border-primary bg-primary px-3 text-sm text-primary-foreground hover:opacity-90"
            >
              <p.icon className="size-4 shrink-0" />
              {p.name}
              <ExternalLinkIcon className="size-3.5" />
            </a>
          ) : canRegister ? (
            <CreateGitHubApp
              key={p.id}
              returnTo={returnTo}
              label={p.name}
              icon={p.icon}
              note={false}
            />
          ) : (
            <span key={p.id} className="text-sm text-muted-foreground">
              {p.name} is not set up on this instance yet, and only a super-admin can do it.
            </span>
          ),
        )}
      </div>
    </div>
  );
}
