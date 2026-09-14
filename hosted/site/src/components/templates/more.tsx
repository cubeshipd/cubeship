"use client";

import { useEffect, useRef, useState } from "react";
import type { TemplateSummary } from "@/lib/catalog";
import { TemplateCard } from "./card";

type Page = { templates: TemplateSummary[]; next_cursor: string | null };

// The pages after the first, loaded as the grid is scrolled to its end.
// The first is rendered on the server, so the page works and is indexed
// without this; `/api/v1` is the catalog, proxied on this origin.
export function MoreTemplates({ query, cursor }: { query: string; cursor: string }) {
  const [rows, setRows] = useState<TemplateSummary[]>([]);
  const [next, setNext] = useState<string | null>(cursor);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const end = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = end.current;
    if (!el || !next || loading || failed) return;
    const seen = new IntersectionObserver(
      async (entries) => {
        if (!entries[0]?.isIntersecting) return;
        seen.disconnect();
        setLoading(true);
        try {
          const params = new URLSearchParams(query);
          params.set("cursor", next);
          const response = await fetch(`/api/v1/templates?${params}`);
          if (!response.ok) throw new Error(String(response.status));
          const page = (await response.json()) as Page;
          setRows((r) => [...r, ...page.templates]);
          setNext(page.next_cursor);
        } catch {
          setFailed(true);
        }
        setLoading(false);
      },
      // A screen ahead, so the next page is usually there before the end is.
      { rootMargin: "600px 0px" },
    );
    seen.observe(el);
    return () => seen.disconnect();
  }, [query, next, loading, failed]);

  return (
    <>
      {rows.length > 0 || loading ? (
        <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {rows.map((template) => (
            <TemplateCard key={`${template.owner}/${template.name}`} template={template} />
          ))}
          {loading
            ? Array.from({ length: 3 }, (_, i) => (
                // biome-ignore lint/suspicious/noArrayIndexKey: placeholders with nothing else to key on.
                <div key={i} className="h-[180px] animate-pulse border border-fd-border bg-card" />
              ))
            : null}
        </div>
      ) : null}
      {failed ? (
        <div className="mt-8 text-center">
          <button
            type="button"
            onClick={() => setFailed(false)}
            className="label border border-fd-border px-4 py-2 text-fd-muted-foreground hover:border-primary hover:text-primary"
          >
            The next page did not load. Try again
          </button>
        </div>
      ) : null}
      <div ref={end} aria-hidden />
    </>
  );
}
