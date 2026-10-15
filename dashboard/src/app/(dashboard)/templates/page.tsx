"use client";

import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { RailTabs } from "@/components/header-rail";
import { SearchBar } from "@/components/search-bar";
import { TemplateCard } from "@/components/template-card";
import { TemplateInstalls } from "@/components/template-installs";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { api, type TemplatePage, type TemplateSummary } from "@/lib/api";
import { message } from "@/lib/errors";

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

  // The catalog is searched on the daemon's side, so a keystroke is a
  // request — and one after the typing pauses is enough.
  useEffect(() => {
    const timer = setTimeout(() => setSearch(query.trim()), 300);
    return () => clearTimeout(timer);
  }, [query]);

  const params = new URLSearchParams();
  if (search) params.set("q", search);

  // Collect the complete result before drawing the grid. The catalog API
  // keeps its cursor contract, but scrolling never triggers another request.
  const page = useQuery({
    queryKey: ["templates", "all", search],
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
        if (!result.next_cursor)
          return [...all.values()].sort((a, b) => a.title.localeCompare(b.title));
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

  const filtered = Boolean(search);

  return (
    <>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <SearchBar
          value={query}
          onChange={setQuery}
          placeholder="Search templates"
          className="min-w-0 flex-1 basis-64"
        />
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
