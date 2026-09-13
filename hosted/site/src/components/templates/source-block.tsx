"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";

// The file itself, plus the address it is read from: the release's
// commit, which is what an instance will fetch too.
export function SourceBlock({ source, rawUrl }: { source: string; rawUrl: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    await navigator.clipboard.writeText(source);
    setCopied(true);
    setTimeout(() => setCopied(false), 1600);
  }

  return (
    <div className="hud-frame border border-fd-border">
      <div className="flex items-center justify-between border-fd-border border-b px-4 py-2">
        <p className="label text-fd-muted-foreground">template.yaml</p>
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
        GET {rawUrl}
      </p>
    </div>
  );
}
