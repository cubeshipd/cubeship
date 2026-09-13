"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";

// The file itself, plus the one address an instance would read instead
// of a person: no install verb is shown, because none exists yet.
export function SourceBlock({ source, manifestUrl }: { source: string; manifestUrl: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    await navigator.clipboard.writeText(source);
    setCopied(true);
    setTimeout(() => setCopied(false), 1600);
  }

  return (
    <div className="hud-frame border border-fd-border">
      <div className="flex items-center justify-between border-fd-border border-b px-4 py-2">
        <p className="label text-fd-muted-foreground">The file</p>
        <button
          type="button"
          onClick={copy}
          aria-label="Copy the template file"
          className="text-fd-muted-foreground transition-colors hover:text-primary"
        >
          {copied ? <Check className="size-4 text-success" /> : <Copy className="size-4" />}
        </button>
      </div>
      <pre className="overflow-x-auto p-4 text-fd-foreground text-xs leading-relaxed">
        <code>{source}</code>
      </pre>
      <p className="overflow-x-auto border-fd-border border-t px-4 py-2 font-mono text-fd-muted-foreground text-xs">
        GET {manifestUrl}
      </p>
    </div>
  );
}
