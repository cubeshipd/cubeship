"use client";

import { cn } from "cn";
import { CheckIcon, ChevronDownIcon } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { SearchBar } from "@/components/search-bar";
import type { Choice } from "@/components/searchable-select";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

const SEARCH_FROM = 8;

// SearchableSelect for more than one: the same field, and a list that
// stays open while you tick. Nothing chosen is a value of its own —
// `none` names what it means.
export function MultiSelect({
  label,
  hint,
  none,
  empty = "Nothing to choose from.",
  choices,
  values,
  onChange,
  disabled,
}: {
  label?: string;
  hint?: string;
  none: string;
  empty?: string;
  choices: Choice[];
  values: string[];
  onChange: (values: string[]) => void;
  disabled?: boolean;
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

  const chosen = choices.filter((c) => values.includes(c.value));
  const toggle = (value: string) =>
    onChange(values.includes(value) ? values.filter((v) => v !== value) : [...values, value]);

  return (
    // A margin on the label rather than space-y: see SearchableSelect.
    <div>
      {label && <Label className="mb-2 block text-xs text-muted-foreground">{label}</Label>}
      <Popover
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (next) {
            setQuery("");
            if (searchable) requestAnimationFrame(() => search.current?.focus());
          }
        }}
      >
        <PopoverTrigger
          render={
            <button
              type="button"
              data-slot="select-trigger"
              disabled={disabled}
              className="flex h-10 w-full items-center justify-between gap-2 border border-border px-3 text-left text-sm disabled:cursor-not-allowed disabled:opacity-50"
            >
              <span
                className={cn(
                  "min-w-0 truncate",
                  chosen.length === 0 ? "text-muted-foreground" : "font-mono text-xs",
                )}
              >
                {chosen.length === 0 ? none : chosen.map((c) => c.label).join(", ")}
              </span>
              <span className="flex shrink-0 items-center gap-2">
                {chosen.length > 1 && (
                  <span className="font-mono text-[11px] text-muted-foreground">
                    {chosen.length}
                  </span>
                )}
                <ChevronDownIcon className="size-4 text-muted-foreground" />
              </span>
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
            <button
              type="button"
              onClick={() => onChange([])}
              className={cn(
                "flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-secondary",
                values.length === 0 && "bg-secondary",
              )}
            >
              <span className="min-w-0 flex-1 truncate text-xs">{none}</span>
              <CheckIcon
                className={cn(
                  "size-3.5 shrink-0",
                  values.length === 0 ? "text-primary" : "invisible",
                )}
              />
            </button>
            <div className="my-1 border-t border-border" />
            {filtered.length === 0 && (
              <p className="px-3 py-6 text-center text-xs text-muted-foreground">
                {choices.length === 0 ? empty : `Nothing matches "${query}".`}
              </p>
            )}
            {filtered.map((choice) => {
              const on = values.includes(choice.value);
              return (
                <button
                  key={choice.value}
                  type="button"
                  aria-pressed={on}
                  onClick={() => toggle(choice.value)}
                  className={cn(
                    "flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-secondary",
                    on && "bg-secondary",
                  )}
                >
                  <span className="min-w-0 flex-1 truncate font-mono text-xs">{choice.label}</span>
                  {choice.hint && (
                    <span className="shrink-0 text-[11px] text-muted-foreground">
                      {choice.hint}
                    </span>
                  )}
                  <CheckIcon
                    className={cn("size-3.5 shrink-0", on ? "text-primary" : "invisible")}
                  />
                </button>
              );
            })}
          </div>
        </PopoverContent>
      </Popover>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}
