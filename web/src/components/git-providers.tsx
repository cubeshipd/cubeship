"use client";

import { CheckIcon, ExternalLinkIcon } from "lucide-react";
import type { ComponentType } from "react";
import { CreateGitHubApp } from "@/components/github-app-manifest";
import { LoadingControl } from "@/components/loading";
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
  checking = false,
}: {
  providers: Provider[];
  // Registering the instance with a provider is the operator's, not an
  // organization admin's. Someone who cannot do it is told, rather than
  // shown a button that fails at the last step.
  canRegister: boolean;
  returnTo?: string;
  // checking is "the daemon has not said yet whether this instance is
  // registered, or where to install".
  //
  // It is a state and not a detail, because of what the button under it
  // is. "Not connected" and "not asked yet" look identical from here,
  // and the button for the first of them **registers a new GitHub
  // App** — which breaks every installation on the old one. Rendering
  // it while the answer is still in flight puts that one click away
  // from somebody who arrived on a screen that was already connected.
  checking?: boolean;
}) {
  if (checking) {
    return (
      <div className="space-y-1.5">
        <Label>Provider</Label>
        {/* Sized like the row it stands in for, so nothing jumps when
            the answer lands. */}
        <LoadingControl className="h-9 w-32" />
      </div>
    );
  }
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
