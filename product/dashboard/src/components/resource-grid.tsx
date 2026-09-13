"use client";

import Link from "next/link";
import { type ComponentType, type ReactNode, useMemo, useState } from "react";
import { SearchBar } from "@/components/search-bar";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { UsageBar } from "@/components/usage-bar";
import type { Shares } from "@/lib/usage";

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
        // Three columns only from xl: below it a card is too narrow for a
        // name beside its mark and a status badge.
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {matched == null
            ? LOADING.map((k) => <LoadingCard key={k} />)
            : matched.map((row) => <div key={rowKey(row)}>{card(row)}</div>)}
        </div>
      )}
    </>
  );
}

const LOADING = ["a", "b", "c"];

const FRAME = "hud-frame flex flex-col border border-border bg-card";
const HEAD = "flex items-center gap-4 p-4";
const FOOT = "grid grid-cols-2 gap-4 border-t border-border px-4 py-2.5";

// One resource: its mark, its name and one line beside it, its state at
// the right edge, and what it is using along the bottom.
export function ResourceCard({
  href,
  icon: Icon,
  mark,
  name,
  detail,
  status,
  usage,
}: {
  href: string;
  icon?: ComponentType<{ className?: string }>;
  // In place of the icon's box, for a mark of its own — a project's picture.
  mark?: ReactNode;
  name: string;
  detail: ReactNode;
  status: ReactNode;
  usage: Shares;
}) {
  return (
    <Link
      href={href}
      className={`${FRAME} group h-full transition-all hover:border-primary/40 hover:bg-secondary/40 focus-visible:border-primary focus-visible:outline-none`}
    >
      <span className={HEAD}>
        {mark ?? (
          <span className="flex size-11 shrink-0 items-center justify-center border border-border bg-primary/5 text-primary/70 group-hover:text-primary">
            {Icon && <Icon className="size-5" />}
          </span>
        )}
        <span className="min-w-0 flex-1">
          <span className="block truncate font-mono text-sm font-semibold group-hover:text-primary">
            {name}
          </span>
          <span className="block h-4 truncate text-xs text-muted-foreground">{detail}</span>
        </span>
        <span className="shrink-0">{status}</span>
      </span>
      <span className={`${FOOT} mt-auto`}>
        <UsageBar label="CPU" percent={usage.cpu} />
        <UsageBar label="Mem" percent={usage.memory} />
      </span>
    </Link>
  );
}

// The same frame and height as a card, so the grid does not move when
// the list arrives.
function LoadingCard() {
  return (
    <div className={FRAME}>
      <span className={HEAD}>
        <Skeleton className="size-11 shrink-0 rounded-none" />
        {/* Each line the height of the text it stands for. */}
        <span className="min-w-0 flex-1">
          <span className="flex h-5 items-center">
            <Skeleton className="h-3.5 w-2/3" />
          </span>
          <span className="flex h-4 items-center">
            <Skeleton className="h-3 w-1/3" />
          </span>
        </span>
      </span>
      <span className={FOOT}>
        <UsageBar label="CPU" />
        <UsageBar label="Mem" />
      </span>
    </div>
  );
}
