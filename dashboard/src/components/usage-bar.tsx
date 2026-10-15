import { cn } from "cn";

// One share of a ceiling as a thin bar, with the percentage after it.
// Nothing running is 0, the same bar empty.
export function UsageBar({ label, percent }: { label: string; percent?: number }) {
  const shown = Math.min(Math.max(percent ?? 0, 0), 100);
  const tone = shown >= 90 ? "destructive" : shown >= 75 ? "warning" : "primary";

  return (
    <span className="flex min-w-0 items-center gap-2">
      <span className="w-7 shrink-0 text-[10px] tracking-[0.12em] text-subtle-foreground uppercase">
        {label}
      </span>
      {/* Hidden from screen readers: the label and the number beside it say it. */}
      <span className="relative h-1 min-w-0 flex-1 overflow-hidden bg-border" aria-hidden="true">
        <span
          className={cn(
            "absolute inset-y-0 left-0 transition-[width] duration-500",
            tone === "destructive" && "bg-destructive",
            tone === "warning" && "bg-warning",
            tone === "primary" && "bg-primary",
          )}
          style={{ width: `${shown}%` }}
        />
      </span>
      <span
        className={cn(
          "w-8 shrink-0 text-right font-mono text-[11px] tabular-nums",
          shown === 0 && "text-muted-foreground",
          shown > 0 && tone === "destructive" && "text-destructive",
          shown > 0 && tone === "warning" && "text-warning",
          shown > 0 && tone === "primary" && "text-foreground",
        )}
      >
        {Math.round(shown)}%
      </span>
    </span>
  );
}
