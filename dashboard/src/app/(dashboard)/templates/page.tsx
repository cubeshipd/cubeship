"use client";

import { useQuery } from "@tanstack/react-query";
import { ChevronDownIcon, LayoutTemplateIcon, PackageCheckIcon, TagsIcon } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import { SearchBar } from "@/components/search-bar";
import { TemplateCard } from "@/components/template-card";
import { TemplateInstalls } from "@/components/template-installs";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { api } from "@/lib/api";
import { message } from "@/lib/errors";
import { fetchTemplates } from "@/lib/templates";

const TABS = ["catalog", "installed"] as const;
type Tab = (typeof TABS)[number];
const CATALOG_SKELETONS = ["one", "two", "three", "four", "five", "six"];

// Two tabs: what can be installed, and what is. The tab is linkable —
// the list of installations is where a breadcrumb over one goes back to.
export default function TemplatesPage() {
  const asked = useSearchParams().get("tab");
  const [tab, setTab] = useState<Tab>(() =>
    TABS.includes(asked as Tab) ? (asked as Tab) : "catalog",
  );
  return (
    <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
      <div className="space-y-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Templates</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Install ready-to-run apps and services, or manage what is already installed.
          </p>
        </div>
        <TabsList variant="default" className="border border-border bg-card">
          <TabsTrigger
            value="catalog"
            className="flex-none px-3 data-active:bg-accent data-active:text-foreground dark:data-active:border-transparent dark:data-active:bg-accent"
          >
            Catalog
          </TabsTrigger>
          <TabsTrigger
            value="installed"
            className="flex-none px-3 data-active:bg-accent data-active:text-foreground dark:data-active:border-transparent dark:data-active:bg-accent"
          >
            Installed
          </TabsTrigger>
        </TabsList>
      </div>
      <TabsContent value="catalog" className="mt-0">
        <Catalog />
      </TabsContent>
      <TabsContent value="installed" className="mt-0">
        <TemplateInstalls />
      </TabsContent>
    </Tabs>
  );
}

function Catalog() {
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState("all");
  const tags = useQuery({
    queryKey: ["template-tags"],
    queryFn: () => api.get<{ tags: string[] }>("/template-tags"),
    staleTime: 5 * 60_000,
    retry: 1,
  });

  const categoryCatalog = useQuery({
    queryKey: ["templates", "all", "", "all"],
    queryFn: ({ signal }) => fetchTemplates("", "all", signal),
    staleTime: 60_000,
    retry: 1,
  });

  // The catalog is searched on the daemon's side, so a keystroke is a
  // request — and one after the typing pauses is enough.
  useEffect(() => {
    const timer = setTimeout(() => setSearch(query.trim()), 300);
    return () => clearTimeout(timer);
  }, [query]);

  // Collect the complete result before drawing the grid. The catalog API
  // keeps its cursor contract, but scrolling never triggers another request.
  const page = useQuery({
    queryKey: ["templates", "all", search, category],
    queryFn: ({ signal }) => fetchTemplates(search, category, signal),
    placeholderData: (previous) => previous,
    staleTime: 60_000,
    retry: 1,
  });
  const templates = page.data;
  const filtered = Boolean(search || category !== "all");
  const counts = new Map<string, number>();
  for (const template of categoryCatalog.data ?? []) {
    for (const tag of template.tags) counts.set(tag, (counts.get(tag) ?? 0) + 1);
  }
  const categoryOptions = [
    {
      value: "all",
      label: <CategoryOption name="All categories" count={categoryCatalog.data?.length ?? 0} />,
      searchText: "All categories",
    },
    ...(tags.data?.tags ?? []).map((tag) => ({
      value: tag,
      label: <CategoryOption name={tag} count={counts.get(tag) ?? 0} />,
      searchText: tag,
    })),
  ];

  return (
    <>
      <div className="mb-4 grid gap-3 sm:grid-cols-[minmax(16rem,1fr)_14rem]">
        <SearchBar
          value={query}
          onChange={setQuery}
          placeholder="Search templates"
          className="min-w-0 flex-1 basis-64"
          count={{
            value: templates?.length ?? 0,
            singular: "template",
            plural: "templates",
            loading: page.isFetching,
          }}
        />
        <GlideSelect
          ariaLabel="Filter templates by category"
          placeholder="All categories"
          startIcon={<TagsIcon className="size-4" />}
          triggerContent={
            <>
              <span className="flex min-w-0 items-center gap-2">
                <TagsIcon className="size-4 shrink-0 text-primary" aria-hidden="true" />
                <span className="truncate">{category === "all" ? "All categories" : category}</span>
              </span>
              <ChevronDownIcon
                className="size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
            </>
          }
          options={categoryOptions}
          value={category}
          onChange={setCategory}
          searchable
          busy={tags.isLoading || categoryCatalog.isLoading}
          emptyMessage="No categories found."
          className="w-full"
          triggerClassName="flex h-[38px] w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        />
      </div>

      <ErrorAlert error={page.error ? message(page.error) : null} />
      <ErrorAlert error={tags.error ? message(tags.error) : null} />
      <ErrorAlert
        error={
          categoryCatalog.error && categoryCatalog.error !== page.error
            ? message(categoryCatalog.error)
            : null
        }
      />
      {templates && templates.length === 0 ? (
        <div className="flex min-h-64 flex-col items-center justify-center rounded-xl border border-border bg-card px-6 py-12 text-center">
          <span className="mb-4 grid size-12 place-items-center rounded-xl border border-border bg-background text-muted-foreground">
            {filtered ? (
              <PackageCheckIcon className="size-5" />
            ) : (
              <LayoutTemplateIcon className="size-5" />
            )}
          </span>
          <h2 className="text-base font-medium">
            {filtered ? "No matching templates" : "The catalog is empty"}
          </h2>
          <p className="mt-1 max-w-sm text-sm text-muted-foreground">
            {filtered
              ? "Try a different search term."
              : "Templates published to this instance will appear here."}
          </p>
        </div>
      ) : (
        <div aria-busy={page.isFetching} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {templates?.map((t) => (
            <TemplateCard key={`${t.owner}/${t.name}`} template={t} />
          ))}
          {!templates &&
            !page.error &&
            CATALOG_SKELETONS.map((key) => (
              <div
                key={key}
                className="h-[210px] animate-pulse rounded-xl border border-border bg-card"
              />
            ))}
        </div>
      )}
    </>
  );
}

function CategoryOption({ name, count }: { name: string; count: number }) {
  return (
    <span className="flex w-full items-center justify-between gap-4">
      <span className="truncate">{name}</span>
      <span className="shrink-0 font-mono text-xs text-muted-foreground">{count}</span>
    </span>
  );
}
