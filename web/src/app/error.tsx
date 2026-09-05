"use client";

import { useEffect } from "react";
import { Button } from "@/components/ui/button";

// What a page renders when its own code throws. It is a client component
// and it must be: Next only hands the error to something that can hold
// state and offer a retry.
//
// The message is shown rather than swallowed. This runs on one operator's
// own instance, and hiding what went wrong from the person who has to fix
// it buys nothing.
export default function ErrorPage({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <main className="flex min-h-screen items-center justify-center bg-background bg-grid px-6">
      <div className="hud-frame w-full max-w-lg bg-card p-8">
        <p className="font-mono text-[11px] tracking-[0.2em] text-destructive uppercase">Error</p>
        <h1 className="mt-3 text-xl font-semibold">This page could not be rendered</h1>
        <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
          Something failed while drawing this screen. The daemon and everything it is running are
          unaffected — only the dashboard stopped here.
        </p>

        <pre className="mt-5 max-h-40 overflow-auto border border-border bg-secondary/40 p-3 font-mono text-[11px] leading-relaxed text-muted-foreground">
          {error.message}
          {error.digest ? `\n\ndigest: ${error.digest}` : ""}
        </pre>

        <div className="mt-6 flex items-center gap-3">
          <Button onClick={reset}>Try again</Button>
          <a
            href="/"
            className="font-mono text-xs text-muted-foreground underline underline-offset-4 hover:text-primary"
          >
            Back to projects
          </a>
        </div>
      </div>
    </main>
  );
}
