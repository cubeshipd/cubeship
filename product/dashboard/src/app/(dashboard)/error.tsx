"use client";

import { useEffect } from "react";
import Link from "@/components/navigation-link";
import { Button } from "@/components/ui/button";

// A failed page stays inside the shell, so another destination is still usable.
export default function DashboardError({
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
    <section className="dashboard-loading-error" role="alert">
      <h1>This page couldn’t load.</h1>
      <p>Try again, or open another screen from the sidebar.</p>
      <details className="my-6 text-xs text-muted-foreground">
        <summary className="cursor-pointer">Technical details</summary>
        <pre className="mt-3 overflow-auto whitespace-pre-wrap break-words font-mono">
          {error.message}
          {error.digest ? `\nReference: ${error.digest}` : ""}
        </pre>
      </details>
      <div className="flex items-center gap-4">
        <Button onClick={reset}>Try again</Button>
        <Link href="/" className="text-sm text-muted-foreground hover:text-primary">
          Back to overview
        </Link>
      </div>
    </section>
  );
}
