"use client";

import { ChevronRightIcon, MoreHorizontalIcon } from "lucide-react";
import type { ReactNode } from "react";
import { GlideSelect } from "@/components/glide-select";
import Link from "@/components/navigation-link";
import { StatusBadge } from "@/components/status-badge";
import { buttonVariants } from "@/components/ui/button";

export type MenuItem = {
  id: string;
  label: string;
  icon?: ReactNode;
  destructive?: boolean;
  onSelect: () => void;
};

// The ⋯ menu: secondary actions that do not deserve a button.
export function ActionMenu({
  label,
  items,
  compact = false,
}: {
  label: string;
  items: MenuItem[];
  // compact is the small ghost trigger a table row uses.
  compact?: boolean;
}) {
  if (items.length === 0) return null;
  return (
    <GlideSelect
      value=""
      ariaLabel={label}
      triggerContent={<MoreHorizontalIcon className="size-4 text-muted-foreground" />}
      triggerClassName={
        compact
          ? "inline-flex size-7 cursor-pointer items-center justify-center rounded-md border-0 bg-transparent p-0 transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
          : buttonVariants({ variant: "outline", size: "icon-lg" })
      }
      align="right"
      options={items.map((item) => ({
        value: item.id,
        label: (
          <span
            className={`inline-flex items-center gap-2 ${item.destructive ? "text-destructive" : ""}`}
          >
            {item.icon}
            {item.label}
          </span>
        ),
      }))}
      onChange={(id) => items.find((item) => item.id === id)?.onSelect()}
    />
  );
}

// The head of a resource page: a quiet trail, a large title with its
// status beside it, and the actions on the right — the main one a primary
// button, secondary ones outline, the rest in `menu`.
export function ResourceHeader({
  crumbs = [],
  title,
  status,
  actions,
  menu,
  menuLabel = "More actions",
}: {
  crumbs?: { label: string; href: string }[];
  title: ReactNode;
  status?: string;
  actions?: ReactNode;
  menu?: MenuItem[];
  menuLabel?: string;
}) {
  return (
    <header className="mb-8 space-y-2">
      {crumbs.length > 0 && (
        <nav aria-label="Breadcrumb" className="text-xs text-muted-foreground">
          <ol className="flex flex-wrap items-center gap-1">
            {crumbs.map((crumb, index) => (
              <li key={crumb.href} className="flex items-center gap-1">
                {index > 0 && <ChevronRightIcon className="size-3" aria-hidden="true" />}
                <Link href={crumb.href} className="transition-colors hover:text-foreground">
                  {crumb.label}
                </Link>
              </li>
            ))}
          </ol>
        </nav>
      )}
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
        <div className="flex min-w-0 items-center gap-3">
          <h1 className="truncate text-3xl font-semibold tracking-tight">{title}</h1>
          {status && <StatusBadge value={status} />}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {actions}
          {menu && <ActionMenu label={menuLabel} items={menu} />}
        </div>
      </div>
    </header>
  );
}
