import type { ReactNode } from "react";

export function PageHeader({
  title,
  sub,
  actions,
  icon,
  literal = false,
  below,
}: {
  title: ReactNode;
  sub?: ReactNode;
  actions?: ReactNode;
  // Something that belongs to the page rather than to what is under it
  // — a filter, say. It sits above the separator, which is what says so.
  below?: ReactNode;
  // A mark before the title, for a page that is about one thing with a
  // logo: a registry, a provider.
  icon?: ReactNode;
  // literal leaves the title as written. Page titles are uppercased as
  // a house style, and that is right for a word and wrong for a value —
  // a hostname is read character by character, and shouting it makes it
  // harder, not louder.
  literal?: boolean;
}) {
  return (
    <header className="mb-7 border-b border-border pb-5">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h1
            className={
              literal
                ? "flex items-center gap-2.5 font-mono text-lg font-semibold break-all"
                : "flex items-center gap-2.5 text-xl font-bold tracking-[0.14em] uppercase"
            }
          >
            {icon}
            {title}
          </h1>
          {sub && <p className="mt-2 max-w-2xl text-sm text-muted-foreground">{sub}</p>}
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </div>
      {below && <div className="mt-5">{below}</div>}
    </header>
  );
}

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
