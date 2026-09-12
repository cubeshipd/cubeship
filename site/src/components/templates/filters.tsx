"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";

const SORTS = [
  { value: "recent", label: "Newest" },
  { value: "likes", label: "Most liked" },
] as const;

// Every filter lives in the query string, not component state, so a
// link to a filtered catalog is a link anyone can share.
export function Filters({ tags }: { tags: string[] }) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [q, setQ] = useState(searchParams.get("q") ?? "");

  const activeTag = searchParams.get("tag");
  const sort = searchParams.get("sort") === "likes" ? "likes" : "recent";

  function push(next: Record<string, string | null>) {
    const params = new URLSearchParams(searchParams.toString());
    params.delete("cursor");
    for (const [key, value] of Object.entries(next)) {
      if (value) params.set(key, value);
      else params.delete(key);
    }
    router.push(params.size > 0 ? `${pathname}?${params}` : pathname);
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-4">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            push({ q: q.trim() || null });
          }}
          className="min-w-0 flex-1"
        >
          <input
            value={q}
            onChange={(event) => setQ(event.target.value)}
            placeholder="Search templates"
            className="w-full max-w-sm border border-border bg-transparent px-3 py-2 text-foreground text-sm outline-none focus:border-primary"
          />
        </form>
        <div className="flex border border-border font-mono text-xs uppercase tracking-wide">
          {SORTS.map((option) => (
            <button
              key={option.value}
              type="button"
              onClick={() => push({ sort: option.value === "recent" ? null : option.value })}
              className={`px-3 py-2 transition-colors ${
                sort === option.value
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-primary"
              }`}
            >
              {option.label}
            </button>
          ))}
        </div>
      </div>
      {tags.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {tags.map((tag) => (
            <button
              key={tag}
              type="button"
              onClick={() => push({ tag: activeTag === tag ? null : tag })}
              className={`label border px-2 py-1 transition-colors ${
                activeTag === tag
                  ? "border-primary text-primary"
                  : "border-border text-muted-foreground hover:border-primary hover:text-primary"
              }`}
            >
              {tag}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
