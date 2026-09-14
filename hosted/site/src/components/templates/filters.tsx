"use client";

import { Popover, PopoverContent, PopoverTrigger } from "fumadocs-ui/components/ui/popover";
import { ChevronDown, Search, Tag } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { Input } from "@/components/ui/field";
import type { Sort } from "@/lib/catalog";

const SORTS = [
  { value: "recent", label: "Newest" },
  { value: "stars", label: "Most starred" },
] as const;

// Every filter lives in the query string, not component state, so a
// link to a filtered catalog is a link anyone can share.
// The page hands the current filters down rather than this reading them with
// useSearchParams, which would need a Suspense boundary and paint the filters
// after the grid below them, shifting it.
export function Filters({
  tags,
  q: initialQ,
  tag: activeTag,
  sort,
}: {
  tags: string[];
  q?: string;
  tag?: string;
  sort: Sort;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const [q, setQ] = useState(initialQ ?? "");

  function push(next: Record<string, string | null>, replace = false) {
    const params = new URLSearchParams();
    if (initialQ) params.set("q", initialQ);
    if (activeTag) params.set("tag", activeTag);
    if (sort !== "recent") params.set("sort", sort);
    params.delete("cursor");
    for (const [key, value] of Object.entries(next)) {
      if (value) params.set(key, value);
      else params.delete(key);
    }
    const url = params.size > 0 ? `${pathname}?${params}` : pathname;
    if (replace) router.replace(url);
    else router.push(url);
  }

  // The URL changing under the field — Back, or a link clearing the
  // filters — is taken in; the URL catching up with the typing is not.
  const sent = useRef(initialQ ?? "");
  useEffect(() => {
    if ((initialQ ?? "") !== sent.current) {
      sent.current = initialQ ?? "";
      setQ(initialQ ?? "");
    }
  }, [initialQ]);

  // Filters while typing, once the typing pauses. Replaced rather than
  // pushed, so Back leaves the catalog instead of stepping through letters.
  useEffect(() => {
    const next = q.trim();
    if (next === sent.current) return;
    const timer = setTimeout(() => {
      sent.current = next;
      push({ q: next || null }, true);
    }, 300);
    return () => clearTimeout(timer);
  });

  return (
    <div className="template-filters">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          sent.current = q.trim();
          push({ q: q.trim() || null });
        }}
        className="template-search"
      >
        <Search
          className="pointer-events-none absolute top-1/2 left-4 size-4 -translate-y-1/2 text-muted-foreground"
          aria-hidden
        />
        <Input
          type="search"
          value={q}
          onChange={(event) => setQ(event.target.value)}
          placeholder="Search by name, description or tag"
          aria-label="Search templates"
          className="h-12 pl-11"
        />
      </form>
      <TagSelect tags={tags} active={activeTag} onChange={(tag) => push({ tag })} />
      <div className="template-sort" aria-label="Sort templates">
        {SORTS.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => push({ sort: option.value === "recent" ? null : option.value })}
            aria-pressed={sort === option.value}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  );
}

// One control however many tags authors coin: a list of chips grows with
// the catalog, a searchable select does not.
function TagSelect({
  tags,
  active,
  onChange,
}: {
  tags: string[];
  active?: string;
  onChange: (tag: string | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const shown = tags.filter((tag) => tag.includes(filter.trim().toLowerCase()));

  function choose(tag: string | null) {
    setOpen(false);
    setFilter("");
    onChange(tag);
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger data-slot="select-trigger" className="template-topic-trigger">
        <Tag className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <span
          className={`min-w-0 flex-1 truncate ${active ? "text-fd-foreground" : "text-muted-foreground"}`}
        >
          {active ?? "All tags"}
        </span>
        <ChevronDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="template-topic-popover w-(--anchor-width) min-w-52 rounded-none border-border bg-fd-background p-0 backdrop-blur-none"
      >
        <div className="relative border-border border-b">
          <Search
            className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
          <input
            // biome-ignore lint/a11y/noAutofocus: the popover opened to type into this.
            autoFocus
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && shown.length > 0) {
                event.preventDefault();
                choose(shown[0]);
              }
            }}
            placeholder="Find a tag"
            aria-label="Find a tag"
            className="h-9 w-full bg-transparent pr-3 pl-9 font-mono text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>
        <ul className="max-h-64 overflow-y-auto py-1">
          {active ? (
            <li>
              <button
                type="button"
                onClick={() => choose(null)}
                className="w-full px-3 py-1.5 text-left font-mono text-muted-foreground text-sm hover:bg-card hover:text-primary"
              >
                Clear tag
              </button>
            </li>
          ) : null}
          {shown.map((tag) => (
            <li key={tag}>
              <button
                type="button"
                onClick={() => choose(tag === active ? null : tag)}
                aria-pressed={tag === active}
                className={`w-full px-3 py-1.5 text-left font-mono text-sm hover:bg-card hover:text-primary ${
                  tag === active ? "text-primary" : "text-fd-foreground"
                }`}
              >
                {tag}
              </button>
            </li>
          ))}
          {shown.length === 0 ? (
            <li className="px-3 py-1.5 text-muted-foreground text-sm">No tag matches.</li>
          ) : null}
        </ul>
      </PopoverContent>
    </Popover>
  );
}
