"use client";

import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { cn } from "cn";
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { LoadingRows } from "@/components/loading";
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
// It exists for one thing the tables here kept getting wrong: **a cell
// with no declared width is a cell that can be any width.** A DKIM
// record's value is two thousand characters, and one of those in a
// column that sizes to its content drags the whole table sideways —
// the header stops lining up with the rows, and everything after it is
// off screen. So the layout is fixed, every column declares a size, and
// what does not fit is truncated rather than allowed to push.
//
// The rest is what TanStack gives for free and what every one of these
// tables was hand-rolling: sorting, and one place for a column to say
// how it renders.
//
// `size` on a column is a **proportion**, not pixels: `table-layout:
// fixed` distributes the declared widths, so what matters is their
// ratio to each other. A column that must not shrink says so with a
// class instead.
export type DataTableColumn<T> = ColumnDef<T> & {
  // Truncate is the default and the reason this component exists. Set
  // it false for a column whose content must wrap instead — a value
  // someone has to read all of.
  wrap?: boolean;
};

export function DataTable<T>({
  columns,
  rows,
  empty,
  loadingRows = 6,
  rowKey,
  onRowClick,
  className,
}: {
  columns: DataTableColumn<T>[];
  // null means still loading, which is not the same as loaded-and-empty
  // and must not look the same.
  rows: T[] | null;
  empty?: ReactNode;
  loadingRows?: number;
  rowKey?: (row: T, index: number) => string;
  onRowClick?: (row: T) => void;
  className?: string;
}) {
  const [sorting, setSorting] = useState<SortingState>([]);

  const table = useReactTable({
    data: rows ?? [],
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });

  if (rows !== null && rows.length === 0 && empty) {
    return (
      <Card>
        <CardContent className="py-2 text-sm text-muted-foreground">{empty}</CardContent>
      </Card>
    );
  }

  return (
    <Card className={cn("py-0", className)}>
      {/* The scroll container is here rather than on the page, so a
          table that is genuinely wider than the pane scrolls inside its
          own card and the page body never does. */}
      <div className="w-full overflow-x-auto">
        <Table className="w-full table-fixed">
          <TableHeader>
            {table.getHeaderGroups().map((group) => (
              <TableRow key={group.id}>
                {group.headers.map((header) => {
                  const sortable = header.column.getCanSort();
                  const direction = header.column.getIsSorted();
                  return (
                    <TableHead
                      key={header.id}
                      className="px-4"
                      style={{ width: `${header.getSize()}%` }}
                    >
                      {header.isPlaceholder ? null : sortable ? (
                        <button
                          type="button"
                          onClick={header.column.getToggleSortingHandler()}
                          className="inline-flex items-center gap-1.5 hover:text-foreground"
                        >
                          {flexRender(header.column.columnDef.header, header.getContext())}
                          {direction === "asc" && <ArrowUpIcon className="size-3" />}
                          {direction === "desc" && <ArrowDownIcon className="size-3" />}
                        </button>
                      ) : (
                        flexRender(header.column.columnDef.header, header.getContext())
                      )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>

          <TableBody>
            {rows === null && <LoadingRows rows={loadingRows} columns={columns.length} />}

            {table.getRowModel().rows.map((row, index) => (
              <TableRow
                key={rowKey ? rowKey(row.original, index) : row.id}
                className={cn("select-none", onRowClick && "cursor-pointer")}
                onClick={onRowClick ? () => onRowClick(row.original) : undefined}
              >
                {row.getVisibleCells().map((cell) => {
                  const wrap = (cell.column.columnDef as DataTableColumn<T>).wrap;
                  return (
                    <TableCell
                      key={cell.id}
                      className={cn("px-4 py-2.5", wrap ? "break-words" : "truncate")}
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  );
                })}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </Card>
  );
}
