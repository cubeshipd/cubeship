"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";

export const installCommand = "curl -fsSL https://cubeship.dev/install.sh | sh";

// The one thing on the page somebody types: shown as a prompt, copied
// as the command.
export function InstallCommand() {
  const [copied, setCopied] = useState(false);

  async function copy() {
    await navigator.clipboard.writeText(installCommand);
    setCopied(true);
    setTimeout(() => setCopied(false), 1600);
  }

  return (
    <div className="hud-frame flex w-full max-w-xl items-center gap-3 border border-border bg-card px-4 py-3 font-mono text-sm">
      <span className="text-magenta select-none">$</span>
      <code className="min-w-0 overflow-x-auto whitespace-nowrap text-foreground">
        {installCommand}
      </code>
      <button
        type="button"
        onClick={copy}
        aria-label="Copy the install command"
        className="ml-auto shrink-0 text-muted-foreground transition-colors hover:text-primary"
      >
        {copied ? <Check className="size-4 text-success" /> : <Copy className="size-4" />}
      </button>
    </div>
  );
}
