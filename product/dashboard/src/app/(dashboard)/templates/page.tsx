"use client";

import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { SearchBar } from "@/components/search-bar";
import { TemplateCard } from "@/components/template-card";
import { api, type TemplatePage } from "@/lib/api";
import { message } from "@/lib/errors";

// The catalog at cubeship.dev, read through this instance.
//
// Cards rather than a table, like projects: what somebody comes here to
// do is recognise an app they already know, and that is an icon and a
// name rather than a column to scan.
export default function TemplatesPage() {
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");

  // The catalog is searched on the daemon's side, so a keystroke is a
  // request — and one after the typing pauses is enough.
  useEffect(() => {
    const timer = setTimeout(() => setSearch(query.trim()), 300);
    return () => clearTimeout(timer);
  }, [query]);

  const page = useQuery({
    queryKey: ["templates", search],
    queryFn: () =>
      api.get<TemplatePage>(search ? `/templates?q=${encodeURIComponent(search)}` : "/templates"),
    placeholderData: (previous) => previous,
  });

  return (
    <>
      <SearchBar
        value={query}
        onChange={setQuery}
        placeholder="Search by name, description or tag"
        className="mb-4"
      />
      <ErrorAlert error={page.error ? message(page.error) : null} />
      {page.data && page.data.templates.length === 0 && (
        <p className="py-10 text-center text-sm text-muted-foreground">
          {search ? "No template matches that search." : "The catalog has no templates yet."}
        </p>
      )}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {page.data
          ? page.data.templates.map((t) => (
              <TemplateCard key={`${t.owner}/${t.name}`} template={t} />
            ))
          : !page.error &&
            Array.from({ length: 6 }, (_, i) => (
              // The size of a card, so the grid does not move when they arrive.
              // biome-ignore lint/suspicious/noArrayIndexKey: placeholders with nothing else to key on.
              <div key={i} className="h-[148px] animate-pulse border border-border bg-card" />
            ))}
      </div>
    </>
  );
}
