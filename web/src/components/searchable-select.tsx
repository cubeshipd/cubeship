"use client";

import { cn } from "cn";
import { CheckIcon, ChevronDownIcon, SearchIcon } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

export type Choice = {
  value: string;
  label: string;
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
            requestAnimationFrame(() => search.current?.focus());
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
              <span className={cn("truncate", !selected && "text-muted-foreground")}>
                {busy ? "Loading…" : (selected?.label ?? placeholder)}
              </span>
              <ChevronDownIcon className="size-4 shrink-0 text-muted-foreground" />
            </button>
          }
        />
        <PopoverContent align="start" className="w-(--anchor-width) p-0">
          <div className="flex items-center gap-2 border-b border-border px-3">
            <SearchIcon className="size-3.5 shrink-0 text-muted-foreground" />
            <Input
              ref={search}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search"
              spellCheck={false}
              className="h-9 border-0 bg-transparent px-0 focus-visible:border-0"
            />
          </div>

          <div className="max-h-64 overflow-y-auto p-1">
            {filtered.length === 0 && (
              <p className="px-3 py-6 text-center text-xs text-muted-foreground">
                {choices.length === 0 ? empty : `Nothing matches “${query}”.`}
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
                <CheckIcon
                  className={cn(
                    "size-3.5 shrink-0",
                    choice.value === value ? "text-primary" : "invisible",
                  )}
                />
                <span className="min-w-0 flex-1 truncate font-mono text-xs">{choice.label}</span>
                {choice.hint && (
                  <span className="shrink-0 text-[11px] text-muted-foreground">{choice.hint}</span>
                )}
              </button>
            ))}
          </div>
        </PopoverContent>
      </Popover>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}
