import type { ReactNode } from "react";

// Select filters sit above the search field. The grid grows to four columns;
// unused columns stay empty.
export function FilterPanel({ children }: { children: ReactNode }) {
  return (
    <div className="mb-4 grid grid-cols-1 gap-3 rounded-xl border border-border bg-card p-3 sm:grid-cols-2 xl:grid-cols-4">
      {children}
    </div>
  );
}
