"use client";

import Link from "next/link";
import { type ComponentType, type ReactNode, useMemo, useState } from "react";
import { SearchBar } from "@/components/search-bar";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

// A grid of cards with the filter DataTable carries: the same field, the
// same gap, the same count, and the same two different sentences for an
// empty list and a filter that matched nothing.
export function ResourceGrid<T>({
  rows,
  search,
  rowKey,
  card,
  empty,
}: {
  rows: T[] | null;
  search: { placeholder: string; by: (row: T) => (string | undefined)[] };
  rowKey: (row: T) => string;
  card: (row: T) => ReactNode;
  empty: ReactNode;
}) {
  const [query, setQuery] = useState("");

  const matched = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!rows || needle === "") return rows;
    return rows.filter((row) =>
      search.by(row).some((part) => part?.toLowerCase().includes(needle)),
    );
  }, [rows, search, query]);

  if (rows != null && rows.length === 0) {
    return (
      <Card>
        <CardContent className="py-2 text-sm text-muted-foreground">{empty}</CardContent>
      </Card>
    );
  }

  return (
    <>
      {/* Held invisible while the list loads, so its arrival does not
          push the grid down. */}
      <SearchBar
        className={rows == null ? "invisible mb-4" : "mb-4"}
        value={query}
        onChange={setQuery}
        placeholder={search.placeholder}
        trailing={
          <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
            {matched?.length ?? 0}/{rows?.length ?? 0}
          </span>
        }
      />

      {matched != null && matched.length === 0 ? (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            Nothing matches that.
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {matched == null
            ? LOADING.map((k) => <LoadingCard key={k} />)
            : matched.map((row) => <div key={rowKey(row)}>{card(row)}</div>)}
        </div>
      )}
    </>
  );
}

const LOADING = ["a", "b", "c"];

const FRAME = "hud-frame flex flex-col gap-3 border border-border bg-card p-4";

// One resource: its mark and state on top, and its name and one line
// under them. Stacked rather than side by side, because beside a status
// badge a name in a third of the pane truncated to four letters.
export function ResourceCard({
  href,
  icon: Icon,
  name,
  detail,
  status,
}: {
  href: string;
  icon: ComponentType<{ className?: string }>;
  name: string;
  detail: ReactNode;
  status: ReactNode;
}) {
  return (
    <Link
      href={href}
      className={`${FRAME} group h-full transition-all hover:border-primary/40 hover:bg-secondary/40 focus-visible:border-primary focus-visible:outline-none`}
    >
      <span className="flex items-start justify-between gap-3">
        <span className="flex size-11 shrink-0 items-center justify-center border border-border bg-primary/5 text-primary/70 group-hover:text-primary">
          <Icon className="size-5" />
        </span>
        {status}
      </span>
      <span className="min-w-0">
        <span className="block truncate font-mono text-sm font-semibold group-hover:text-primary">
          {name}
        </span>
        <span className="block truncate text-xs text-muted-foreground">{detail}</span>
      </span>
    </Link>
  );
}

// The same frame and height as a card, so the grid does not move when
// the list arrives.
function LoadingCard() {
  return (
    <div className={FRAME}>
      <Skeleton className="size-11 shrink-0 rounded-none" />
      {/* Each line the height of the text it stands for. */}
      <span className="block">
        <span className="flex h-5 items-center">
          <Skeleton className="h-3.5 w-2/3" />
        </span>
        <span className="flex h-4 items-center">
          <Skeleton className="h-3 w-1/3" />
        </span>
      </span>
    </div>
  );
}
