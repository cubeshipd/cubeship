"use client";

import {
  createSortedRowModel,
  rowSortingFeature,
  type SortingState,
  tableFeatures,
  useTable,
} from "@tanstack/react-table";
import { cn } from "cn";
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";
import { LoadingRows } from "@/components/loading";
import { SearchBar } from "@/components/search-bar";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

// Every table in the dashboard, over TanStack Table.
//
// It exists for one thing the hand-rolled tables here kept getting
// wrong: **a cell with no declared width is a cell that can be any
// width.** A DKIM record's value is two thousand characters, and one of
// those in a column that sizes to its content drags the whole table
// sideways — the header stops lining up with the rows, and everything
// after it is off screen. So the layout is fixed, every column declares
// a width, and what does not fit is truncated rather than allowed to
// push.
//
// The rest is what TanStack gives and what each of these tables was
// otherwise reimplementing: sorting, and one place for a column to say
// how it renders.
//
// Features are registered explicitly in v9 — sorting state does not
// exist until the feature is — and this is the set every table here
// gets. Adding one is an edit in this file, not a per-page option.
const features = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
});

// A fresh array every render invalidates every data-dependent model, so
// the empty case is one value rather than one per render.
const NO_ROWS: never[] = [];

// Column is deliberately not TanStack's ColumnDef spelled out: what a
// page here needs to say about a column is its heading, how to read a
// value out of a row, how wide it is and whether it wraps.
export type Column<T extends object> = {
  // id doubles as the sort key and the React key.
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  // width is a **proportion**, not pixels: `table-layout: fixed`
  // distributes the declared widths, so what matters is their ratio to
  // each other. They should add up to 100.
  width: number;
  // sortBy makes the column sortable. Without it the header is a label.
  sortBy?: (row: T) => string | number;
  // wrap is for a column whose content must be read in full — a value,
  // a key. Everything else truncates, which is the point of this file.
  wrap?: boolean;
  align?: "left" | "right";
};

export function DataTable<T extends object>({
  columns,
  rows,
  empty,
  search,
  loadingRows = 6,
  rowKey,
  onRowClick,
  className,
  maxHeight,
}: {
  columns: Column<T>[];
  // null means still loading, which is not the same as loaded-and-empty
  // and must not look the same.
  // Null is "not loaded yet", which is what draws the skeleton.
  //
  // Undefined is read the same way rather than crashing, and that is
  // not defensiveness for its own sake: every table here is handed a
  // field off a response, so a daemon a version ahead or behind — or a
  // key renamed — arrives as `undefined` at exactly this line. A whole
  // screen replaced by a stack trace is a worse answer to that than a
  // table that keeps waiting, and the one that told us was a fixture
  // claiming `/users` answers an array when it answers an object with
  // an array in it.
  rows: T[] | null | undefined;
  empty?: ReactNode;
  // search puts a filter above the table and answers it here.
  //
  // **In the table rather than beside it**, because it was beside it
  // four times and came out four ways: three with no gap under it and
  // one with a smaller gap than the rest, each with its own `useState`,
  // its own count and its own word for "no match". A filter over a list
  // is the same control every time; what differs is which of a row's
  // words it looks at, which is all a caller says.
  search?: {
    placeholder?: string;
    // The row's text, in parts so a caller does not have to join them.
    // Undefined entries are skipped, which is what an absent field is.
    by: (row: T) => (string | undefined)[];
  };
  loadingRows?: number;
  rowKey?: (row: T, index: number) => string;
  onRowClick?: (row: T) => void;
  className?: string;
  // maxHeight bounds the table and scrolls it inside itself, for a list
  // long enough that the page would otherwise be the list — fifty
  // environment variables above everything else on the screen. The
  // header sticks only then: without a scroll container of its own it
  // would stick to the viewport instead, which is a different and worse
  // thing to do to every other table here.
  maxHeight?: string;
}) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const [query, setQuery] = useState("");

  // Matched here rather than by the caller, over the parts it named.
  // Case-insensitive substring, which is what somebody typing three
  // letters of a name means — no tokens, no ranking, nothing to learn.
  const matched = useMemo(() => {
    if (!rows || !search || query.trim() === "") return rows ?? null;
    const needle = query.trim().toLowerCase();
    return rows.filter((row) =>
      search.by(row).some((part) => part !== undefined && part.toLowerCase().includes(needle)),
    );
  }, [rows, search, query]);

  const table = useTable({
    features,
    data: matched ?? NO_ROWS,
    columns: columns.map((c) => ({
      id: c.id,
      header: () => c.header,
      accessorFn: (row: T) => (c.sortBy ? c.sortBy(row) : ""),
      cell: ({ row }: { row: { original: T } }) => c.cell(row.original),
      enableSorting: c.sortBy !== undefined,
    })),
    state: { sorting },
    onSortingChange: setSorting,
  });

  // The filter is offered once there is something to filter: while the
  // rows are still coming there is nothing to type against, and on an
  // empty list it is a control that can only ever return the same
  // nothing.
  const filter = search && rows != null && rows.length > 0 && (
    <SearchBar
      className="mb-4"
      value={query}
      onChange={setQuery}
      placeholder={search.placeholder ?? "Filter"}
      trailing={
        <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
          {matched?.length ?? 0}/{rows.length}
        </span>
      }
    />
  );

  if (rows != null && rows.length === 0 && empty) {
    return (
      <Card>
        <CardContent className="py-2 text-sm text-muted-foreground">{empty}</CardContent>
      </Card>
    );
  }

  // A filter that matched nothing is a different sentence from a list
  // with nothing in it, and saying the second here would read as the
  // list having emptied itself.
  if (matched != null && matched.length === 0) {
    return (
      <>
        {filter}
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            Nothing matches that.
          </CardContent>
        </Card>
      </>
    );
  }

  return (
    <>
      {filter}
      <Card className={cn("py-0", className)}>
        {/* The scroll container is here rather than on the page, so a
          table that is genuinely wider than the pane scrolls inside its
          own card and the page body never does. */}
        <div
          className={cn("w-full overflow-x-auto", maxHeight && "overflow-y-auto")}
          style={maxHeight ? { maxHeight } : undefined}
        >
          <Table className="w-full table-fixed">
            <TableHeader>
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id}>
                  {group.headers.map((header) => {
                    const column = columns.find((c) => c.id === header.column.id);
                    if (!column) return null;
                    const direction = header.column.getIsSorted?.();
                    return (
                      <TableHead
                        key={header.id}
                        className={cn(
                          "px-4",
                          column.align === "right" && "text-right",
                          maxHeight && "sticky top-0 z-10 bg-card",
                        )}
                        style={{ width: `${column.width}%` }}
                      >
                        {column.sortBy ? (
                          // The slot is what globals.css styles it
                          // through. A button is one of the two elements
                          // Tailwind's preflight resets `text-transform`
                          // on, and an explicit rule beats what the
                          // header cell would otherwise pass down — so a
                          // sortable column's heading came out in mixed
                          // case beside its uppercase neighbours.
                          <button
                            type="button"
                            data-slot="table-head-sort"
                            onClick={header.column.getToggleSortingHandler?.()}
                            className="inline-flex items-center gap-1.5 hover:text-foreground"
                          >
                            {column.header}
                            {direction === "asc" && <ArrowUpIcon className="size-3" />}
                            {direction === "desc" && <ArrowDownIcon className="size-3" />}
                          </button>
                        ) : (
                          column.header
                        )}
                      </TableHead>
                    );
                  })}
                </TableRow>
              ))}
            </TableHeader>

            <TableBody>
              {rows == null && <LoadingRows rows={loadingRows} columns={columns.length} />}

              {table.getRowModel().rows.map((row, index) => (
                <TableRow
                  key={rowKey ? rowKey(row.original, index) : row.id}
                  className={cn("select-none", onRowClick && "cursor-pointer")}
                  onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                >
                  {columns.map((column) => (
                    <TableCell
                      key={column.id}
                      className={cn(
                        "px-4 py-2.5",
                        // whitespace-normal is not decoration: the cell
                        // primitive carries `whitespace-nowrap`, so
                        // `break-words` on its own never wrapped anything
                        // and every column that asked to wrap quietly ran
                        // off the side instead — a build's error, a DKIM
                        // record's value, a store's endpoint.
                        column.wrap ? "wrap-break-word whitespace-normal" : "truncate",
                        column.align === "right" && "text-right",
                      )}
                    >
                      {column.cell(row.original)}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>
    </>
  );
}
