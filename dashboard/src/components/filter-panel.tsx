import type { ReactNode } from "react";

// Select filters sit above the search field, in one row that wraps. Their
// labels stay for screen readers; the placeholder already names each one.
export function FilterPanel({ children }: { children: ReactNode }) {
  return (
    <div className="mb-3 grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-4 [&_[data-slot=label]]:sr-only">
      {children}
    </div>
  );
}
