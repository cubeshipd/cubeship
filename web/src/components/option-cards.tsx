"use client";

import { cn } from "cn";
import type { ReactNode } from "react";
import { Label } from "@/components/ui/label";

// A choice with consequences, laid out as things to read rather than
// lines in a dropdown. Used where picking wrong is expensive and the
// difference is a sentence, not a word — where an app's image comes
// from, and how it gets built.
export function OptionCards<T extends string>({
  label,
  hint,
  value,
  onChange,
  options,
  className,
}: {
  label?: string;
  hint?: ReactNode;
  value: T;
  onChange: (v: T) => void;
  options: { value: T; title: string; body: ReactNode }[];
  className?: string;
}) {
  return (
    <div className="space-y-2">
      {label && <Label className="text-xs text-muted-foreground">{label}</Label>}
      <div className={cn("grid gap-2 sm:grid-cols-2", className)}>
        {options.map((o) => {
          const selected = o.value === value;
          return (
            <button
              key={o.value}
              type="button"
              onClick={() => onChange(o.value)}
              aria-pressed={selected}
              className={cn(
                "border p-3 text-left transition-all",
                selected
                  ? "neon-edge border-primary/60 bg-primary/8"
                  : "border-border bg-background hover:border-border-strong",
              )}
            >
              <span className="flex items-center gap-2 text-sm font-medium">
                <span
                  aria-hidden="true"
                  className={cn(
                    "size-3 rounded-full border",
                    selected ? "border-primary bg-primary/40" : "border-border-strong",
                  )}
                />
                {o.title}
              </span>
              <span className="mt-1.5 block text-xs leading-relaxed text-muted-foreground">
                {o.body}
              </span>
            </button>
          );
        })}
      </div>
      {hint && <p className="text-xs leading-relaxed text-subtle-foreground">{hint}</p>}
    </div>
  );
}
