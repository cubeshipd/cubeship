"use client";

import Link from "next/link";
import { useEffect } from "react";
import { docsRoute } from "@/lib/shared";

// Next mounts this in place of the segment when a Server Component
// under /templates throws — in practice always the catalog's database
// being unreachable. No retry button: the same request would just hit
// the same database again.
export default function TemplatesError({ error }: { error: Error & { digest?: string } }) {
  useEffect(() => {
    console.error("templates segment error:", error.message);
  }, [error]);

  return (
    <div className="mx-auto max-w-6xl px-6 py-24 text-center">
      <p className="label text-primary">Templates</p>
      <h1 className="mt-2 font-semibold text-2xl text-fd-foreground tracking-tight">
        The template registry is unavailable right now
      </h1>
      <p className="mt-3 text-fd-muted-foreground text-sm">
        Try again in a moment. The docs don't need it.
      </p>
      <Link href={docsRoute} className="label mt-6 inline-block text-primary hover:text-glow">
        Back to the docs →
      </Link>
    </div>
  );
}
