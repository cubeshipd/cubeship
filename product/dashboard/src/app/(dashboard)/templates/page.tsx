"use client";

import { useQuery } from "@tanstack/react-query";
import { cn } from "cn";
import { ChevronDownIcon, TagIcon } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { RailTabs } from "@/components/header-rail";
import { SearchBar } from "@/components/search-bar";
import { TemplateCard } from "@/components/template-card";
import { TemplateInstalls } from "@/components/template-installs";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { api, type TemplatePage, type TemplateSummary } from "@/lib/api";
import { message } from "@/lib/errors";

type Sort = "recent" | "stars";

const SORTS: { value: Sort; label: string }[] = [
  { value: "recent", label: "Newest" },
  { value: "stars", label: "Most starred" },
];

// The catalog at cubeship.dev, read through this instance, with the same
// filters the site has: a search, a tag and an order.
//
// Cards rather than a table, like projects: what somebody comes here to
// do is recognise an app they already know, and that is an icon and a
// name rather than a column to scan.
const TABS = ["catalog", "installed"] as const;
type Tab = (typeof TABS)[number];

// Two tabs: what can be installed, and what is. The tab is linkable —
// the list of installations is where a breadcrumb over one goes back to.
export default function TemplatesPage() {
  const asked = useSearchParams().get("tab");
  const [tab, setTab] = useState<Tab>(() =>
    TABS.includes(asked as Tab) ? (asked as Tab) : "catalog",
  );
  return (
    <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="subrail-page">
      <RailTabs>
        <TabsList variant="line">
          <TabsTrigger value="catalog">Catalog</TabsTrigger>
          <TabsTrigger value="installed">Installed</TabsTrigger>
        </TabsList>
      </RailTabs>
      <TabsContent value="catalog">
        <Catalog />
      </TabsContent>
      <TabsContent value="installed">
        <TemplateInstalls />
      </TabsContent>
    </Tabs>
  );
}

function Catalog() {
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [tag, setTag] = useState<string | null>(null);
  const [sort, setSort] = useState<Sort>("recent");

  // The catalog is searched on the daemon's side, so a keystroke is a
  // request — and one after the typing pauses is enough.
  useEffect(() => {
    const timer = setTimeout(() => setSearch(query.trim()), 300);
    return () => clearTimeout(timer);
  }, [query]);

  const params = new URLSearchParams();
  if (search) params.set("q", search);
  if (tag) params.set("tag", tag);
  if (sort !== "recent") params.set("sort", sort);

  // Collect the complete result before drawing the grid. The catalog API
  // keeps its cursor contract, but scrolling never triggers another request.
  const page = useQuery({
    queryKey: ["templates", "all", search, tag, sort],
    queryFn: async ({ signal }) => {
      const q = new URLSearchParams(params);
      q.set("limit", "48");
      const all = new Map<string, TemplateSummary>();
      const visited = new Set<string>();
      for (;;) {
        const result = await api.get<TemplatePage>(`/templates?${q}`, { signal });
        for (const template of result.templates) {
          const key = `${template.owner}/${template.name}`;
          if (!all.has(key)) all.set(key, template);
        }
        if (!result.next_cursor) return [...all.values()];
        if (visited.has(result.next_cursor))
          throw new Error("The catalog could not finish loading. Please try again.");
        visited.add(result.next_cursor);
        q.set("cursor", result.next_cursor);
      }
    },
    placeholderData: (previous) => previous,
    staleTime: 60_000,
    retry: 1,
  });
  const templates = page.data;

  const tags = useQuery({
    queryKey: ["template-tags"],
    queryFn: () => api.get<{ tags: string[] }>("/template-tags"),
  });

  const filtered = Boolean(search || tag);

  return (
    <>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <SearchBar
          value={query}
          onChange={setQuery}
          placeholder="Search by name, description or tag"
          className="min-w-0 flex-1 basis-64"
        />
        <TagFilter tags={tags.data?.tags ?? []} value={tag} onChange={setTag} />
        <div className="flex h-10 border border-border font-mono text-xs uppercase tracking-wide">
          {SORTS.map((option) => (
            <button
              key={option.value}
              type="button"
              aria-pressed={sort === option.value}
              onClick={() => setSort(option.value)}
              className={cn(
                "px-3 transition-colors",
                sort === option.value
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-primary",
              )}
            >
              {option.label}
            </button>
          ))}
        </div>
      </div>

      <ErrorAlert error={page.error ? message(page.error) : null} />
      {templates && templates.length === 0 && (
        <p className="py-10 text-center text-sm text-muted-foreground">
          {filtered ? "No template matches these filters." : "The catalog has no templates yet."}
        </p>
      )}
      <p role="status" className="mb-4 font-mono text-xs text-muted-foreground">
        {page.isFetching
          ? "Loading catalog…"
          : templates
            ? `${templates.length} ${templates.length === 1 ? "template" : "templates"}`
            : ""}
      </p>
      <div aria-busy={page.isFetching} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {templates?.map((t) => (
          <TemplateCard key={`${t.owner}/${t.name}`} template={t} />
        ))}
        {!templates &&
          !page.error &&
          Array.from({ length: 6 }, (_, i) => (
            // The size of a card, so the grid does not move when they arrive.
            // biome-ignore lint/suspicious/noArrayIndexKey: placeholders with nothing else to key on.
            <div key={i} className="h-[148px] animate-pulse border border-border bg-card" />
          ))}
      </div>
    </>
  );
}

// One control however many tags authors coin: a list of chips grows with
// the catalog, a select with a search does not.
function TagFilter({
  tags,
  value,
  onChange,
}: {
  tags: string[];
  value: string | null;
  onChange: (tag: string | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const [find, setFind] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const shown = tags.filter((t) => t.includes(find.trim().toLowerCase()));

  function choose(next: string | null) {
    setOpen(false);
    onChange(next);
  }

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) {
          setFind("");
          requestAnimationFrame(() => input.current?.focus());
        }
      }}
    >
      <PopoverTrigger
        render={
          <button
            type="button"
            data-slot="select-trigger"
            className="flex h-10 w-52 items-center gap-2 border border-border px-3 text-left font-mono text-sm"
          >
            <TagIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
            <span className={cn("min-w-0 flex-1 truncate", !value && "text-muted-foreground")}>
              {value ?? "All tags"}
            </span>
            <ChevronDownIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          </button>
        }
      />
      <PopoverContent align="start" className="w-(--anchor-width) min-w-52 p-0">
        <SearchBar
          inputRef={input}
          value={find}
          onChange={setFind}
          placeholder="Find a tag"
          className="border-0 border-b focus-within:ring-0"
        />
        <div className="max-h-64 overflow-y-auto p-1">
          {value && (
            <button
              type="button"
              onClick={() => choose(null)}
              className="w-full px-3 py-2 text-left font-mono text-xs text-muted-foreground hover:bg-secondary hover:text-primary"
            >
              Clear tag
            </button>
          )}
          {shown.map((t) => (
            <button
              key={t}
              type="button"
              aria-pressed={t === value}
              onClick={() => choose(t === value ? null : t)}
              className={cn(
                "w-full px-3 py-2 text-left font-mono text-xs hover:bg-secondary",
                t === value ? "text-primary" : "text-foreground",
              )}
            >
              {t}
            </button>
          ))}
          {shown.length === 0 && (
            <p className="px-3 py-6 text-center text-xs text-muted-foreground">
              {tags.length === 0 ? "The catalog has no tags yet." : "No tag matches."}
            </p>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
