"use client";

import { cn } from "cn";
import { CheckIcon, ChevronDownIcon } from "lucide-react";
import { type ComponentType, useMemo, useRef, useState } from "react";
import { SearchBar } from "@/components/search-bar";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

// Below this many options the list is something you scan, and a search
// field is furniture: a bordered box with its own focus ring, inside a
// popup that is already one, around three items.
const SEARCH_FROM = 8;

export type Choice = {
  value: string;
  label: string;
  icon?: ComponentType<{ className?: string }>;
  // hint sits to the right of the label: a default branch, a "private"
  // marker — something that distinguishes two similarly named things.
  hint?: string;
};

// A select you can type into.
//
// The plain one is right for four environments and wrong for two hundred
// repositories: at that length the list stops being something you scan
// and becomes something you search. Filtering is a plain substring match
// on both halves of "owner/name", because that is how someone remembers
// a repository they are looking for.
export function SearchableSelect({
  label,
  hint,
  placeholder = "Select…",
  empty = "Nothing to choose from.",
  choices,
  value,
  onChange,
  disabled,
  busy,
}: {
  label: string;
  hint?: string;
  placeholder?: string;
  empty?: string;
  choices: Choice[];
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  busy?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const search = useRef<HTMLInputElement>(null);

  const searchable = choices.length >= SEARCH_FROM;

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return choices;
    return choices.filter(
      (c) => c.label.toLowerCase().includes(q) || c.value.toLowerCase().includes(q),
    );
  }, [choices, query]);

  const selected = choices.find((c) => c.value === value);

  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <Popover
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (next) {
            setQuery("");
            // The field is why this exists; landing anywhere else means
            // typing does nothing.
            if (searchable) requestAnimationFrame(() => search.current?.focus());
          }
        }}
      >
        <PopoverTrigger
          render={
            <button
              type="button"
              disabled={disabled || busy}
              className={cn(
                "flex h-9 w-full items-center justify-between gap-2 border border-border bg-input px-3 text-left text-sm",
                "hover:border-ring focus-visible:border-ring focus-visible:outline-none",
                "disabled:cursor-not-allowed disabled:opacity-50",
              )}
            >
              <span className="flex min-w-0 items-center gap-2">
                {selected?.icon && <selected.icon className="size-4 shrink-0" />}
                <span className={cn("truncate", !selected && "text-muted-foreground")}>
                  {busy ? "Loading…" : (selected?.label ?? placeholder)}
                </span>
              </span>
              <ChevronDownIcon className="size-4 shrink-0 text-muted-foreground" />
            </button>
          }
        />
        <PopoverContent align="start" className="w-(--anchor-width) p-0">
          {searchable && (
            <SearchBar
              inputRef={search}
              value={query}
              onChange={setQuery}
              placeholder="Search"
              className="border-0 border-b focus-within:ring-0"
            />
          )}

          <div className="max-h-64 overflow-y-auto p-1">
            {filtered.length === 0 && (
              <p className="px-3 py-6 text-center text-xs text-muted-foreground">
                {choices.length === 0 ? empty : `Nothing matches "${query}".`}
              </p>
            )}
            {filtered.map((choice) => (
              <button
                key={choice.value}
                type="button"
                onClick={() => {
                  onChange(choice.value);
                  setOpen(false);
                }}
                className={cn(
                  "flex w-full items-center gap-2 px-3 py-2 text-left text-sm",
                  "hover:bg-secondary",
                  choice.value === value && "bg-secondary",
                )}
              >
                {choice.icon && <choice.icon className="size-4 shrink-0" />}
                <span className="min-w-0 flex-1 truncate font-mono text-xs">{choice.label}</span>
                {choice.hint && (
                  <span className="shrink-0 text-[11px] text-muted-foreground">{choice.hint}</span>
                )}
                {/* At the end, so the marks line up with each other and
                    the labels line up with each other. Leading it put a
                    gap in front of every row that was not selected. */}
                <CheckIcon
                  className={cn(
                    "size-3.5 shrink-0",
                    choice.value === value ? "text-primary" : "invisible",
                  )}
                />
              </button>
            ))}
          </div>
        </PopoverContent>
      </Popover>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}
