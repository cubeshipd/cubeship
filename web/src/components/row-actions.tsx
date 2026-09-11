"use client";

import { cn } from "cn";
import { MoreHorizontalIcon } from "lucide-react";
import type { ComponentType, ReactNode } from "react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

// The buttons at the end of a table row.
//
// One component because they were being written out per page, and the
// difference showed: some rows lit up under the pointer and some did
// not, which reads as "this one is not a button". The hover is the
// whole affordance — an icon on its own says nothing about being
// pressable — so it is decided here rather than by whoever writes the
// next table.
//
// A row is usually a link to somewhere, so every action stops the click
// going any further: pressing Delete is not also asking for the page
// behind it. Stopped on the button rather than on a wrapper, which
// would be a click handler on something nothing can focus.
export function RowActions({ children }: { children: React.ReactNode }) {
  return <span className="flex items-center justify-end gap-1">{children}</span>;
}

export function RowAction({
  icon: Icon,
  label,
  onClick,
  href,
  // danger is for what cannot be undone. It hovers red — the one place
  // a colour is spent on a row action, so it means something when it
  // appears.
  danger = false,
  disabled,
  title,
}: {
  icon: ComponentType<{ className?: string }>;
  // What a screen reader says, and what the tooltip says when there is
  // no other title. An icon with no name is a button nobody can name.
  label: string;
  onClick?: () => void;
  // href turns it into a link out — configuring a connection on the
  // provider's own site, say.
  href?: string;
  danger?: boolean;
  disabled?: boolean;
  title?: string;
}) {
  const className = cn(
    "text-muted-foreground",
    danger ? "hover:text-destructive" : "hover:text-foreground",
  );

  if (href) {
    return (
      <Button
        variant="ghost"
        size="icon-sm"
        nativeButton={false}
        aria-label={label}
        title={title ?? label}
        className={className}
        render={
          <a
            href={href}
            target="_blank"
            rel="noreferrer noopener"
            onClick={(e) => e.stopPropagation()}
          >
            <Icon className="size-3.5" />
          </a>
        }
      />
    );
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label={label}
      title={title ?? label}
      disabled={disabled}
      className={className}
      onClick={(e) => {
        e.stopPropagation();
        onClick?.();
      }}
    >
      <Icon className="size-3.5" />
    </Button>
  );
}

// RowMenu is what a row gets when it has more to offer than a row of
// icons can carry.
//
// **Three icons is a row of buttons; six is a puzzle.** An icon says
// what it does only to somebody who already knows, and the affordance
// stops paying for itself somewhere around the fourth — which is where
// an account's row landed once it could be blocked, have its role
// changed and have a password issued, on top of revoking and deleting.
// Behind one trigger they are words again.
//
// It is here rather than in the page for the reason RowActions is: the
// next table that outgrows five icons should not invent a second answer
// that hovers differently.
export function RowMenu({ label, children }: { label: string; children: ReactNode }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={label}
            title={label}
            className="text-muted-foreground hover:text-foreground"
            // A row is usually a link to somewhere; opening its menu is
            // not also asking for the page behind it.
            onClick={(e) => e.stopPropagation()}
          >
            <MoreHorizontalIcon className="size-3.5" />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-56">
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function RowMenuItem({
  icon: Icon,
  children,
  onClick,
  // danger is for what cannot be undone, and it is the same one colour
  // RowAction spends — so it still means something when it appears.
  danger = false,
  disabled,
  title,
}: {
  icon: ComponentType<{ className?: string }>;
  children: ReactNode;
  onClick?: () => void;
  danger?: boolean;
  disabled?: boolean;
  // Why it is disabled. A disabled item that explains nothing is one
  // somebody clicks twice.
  title?: string;
}) {
  return (
    <DropdownMenuItem
      disabled={disabled}
      title={title}
      className={cn(danger && "text-destructive")}
      onClick={onClick}
    >
      <Icon />
      {children}
    </DropdownMenuItem>
  );
}
