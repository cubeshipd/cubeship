import type { ReactNode } from "react";

// The second heading level: above a card or a table, never above a
// page.
//
// **There is no first level here any more.** A page's title was a
// `PageHeader` at the top of every screen, and of the thirty-one it
// drew almost every one was either the section the sidebar already
// highlights or the last word of the path the rail carries — said
// twice, in two faces, thirty pixels apart. The rail says it once, with
// weight, and what used to sit beside it goes through `RailPortal`.
//
// The heading for a section inside a page — the second level, above a
// card or a table rather than above the page. The accent tick is what
// separates it from the page title at a glance.
export function SectionHeader({
  title,
  sub,
  actions,
}: {
  title: ReactNode;
  sub?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="mt-9 mb-3 flex items-end justify-between gap-4">
      <div className="min-w-0">
        <h2 className="flex items-center gap-2 text-xs font-semibold tracking-[0.16em] uppercase">
          <span
            aria-hidden="true"
            className="h-3 w-0.5 bg-primary shadow-[0_0_8px_var(--primary)]"
          />
          {title}
        </h2>
        {sub && <p className="mt-1.5 text-xs text-muted-foreground">{sub}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
