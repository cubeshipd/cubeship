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
    <div className="editorial-page site-container templates-error">
      <p className="section-kicker">Catalog connection</p>
      <h1>The template catalog is unavailable right now.</h1>
      <p>Try again in a moment. Documentation remains available while the catalog reconnects.</p>
      <Link href={docsRoute} className="inline-link">
        Read the docs <span aria-hidden>↗</span>
      </Link>
    </div>
  );
}
