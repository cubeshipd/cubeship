"use client";

import { cn } from "cn";
import { SearchIcon, XIcon } from "lucide-react";
import type { ReactNode, Ref } from "react";

// A filter field: a mark, a field, and whatever the caller wants on the
// right — usually how much of the list survived.
//
// It uses a bare <input> rather than the Input primitive on purpose.
// That one carries its own border, its own background and its own focus
// ring, and this control needs the ring on the whole thing: the mark and
// the count are part of it, and a ring around only the middle reads as a
// mistake. Neutralising those classes one at a time is a fight the
// primitive wins again on its next upgrade.
export function SearchBar({
  value,
  onChange,
  placeholder = "Filter",
  trailing,
  autoFocus,
  inputRef,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  trailing?: ReactNode;
  autoFocus?: boolean;
  inputRef?: Ref<HTMLInputElement>;
  className?: string;
}) {
  return (
    // Focus is decided in globals.css, with the other fields, so this
    // control lights up the same way they do. A ring here would be a
    // second answer to a question the house style already answers —
    // which is how it came to draw two borders at once.
    <div
      data-slot="search-bar"
      className={cn(
        "flex items-center gap-2 border border-border px-3 transition-colors",
        className,
      )}
    >
      <SearchIcon className="size-3.5 shrink-0 text-muted-foreground" />
      <input
        ref={inputRef}
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        spellCheck={false}
        autoFocus={autoFocus}
        className="h-9 min-w-0 flex-1 bg-transparent font-mono text-sm outline-none placeholder:font-sans placeholder:text-muted-foreground"
      />
      {value && (
        <button
          type="button"
          onClick={() => onChange("")}
          aria-label="Clear"
          className="shrink-0 text-muted-foreground hover:text-foreground"
        >
          <XIcon className="size-3.5" />
        </button>
      )}
      {trailing}
    </div>
  );
}
