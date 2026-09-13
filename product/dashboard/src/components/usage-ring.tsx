import { cn } from "cn";

// At r = 15.9155 the circumference is 100, so the dash is the percentage.
const R = 15.9155;

// CPU and memory, side by side.
export function UsageRings({
  name,
  shares,
}: {
  name: string;
  shares: { cpu?: number; memory?: number };
}) {
  return (
    <>
      <UsageRing percent={shares.cpu} label="CPU" title={`${name} CPU`} />
      <UsageRing percent={shares.memory} label="Mem" title={`${name} memory`} />
    </>
  );
}

// One share of a ceiling as a thin donut: the percentage in the middle
// and what it is a share of under it.
export function UsageRing({
  percent,
  label,
  title,
}: {
  // Of the ceiling, 0–100. Undefined while there is no reading.
  percent?: number;
  label: string;
  title: string;
}) {
  const shown = percent == null ? undefined : Math.min(Math.max(percent, 0), 100);
  const tone =
    shown == null
      ? "text-muted-foreground"
      : shown >= 90
        ? "text-destructive"
        : shown >= 75
          ? "text-warning"
          : "text-primary";

  return (
    <span className="flex flex-col items-center gap-1" role="img" aria-label={title}>
      <span className="relative inline-flex size-11 items-center justify-center">
        <svg viewBox="0 0 36 36" className="size-full -rotate-90">
          <title>{title}</title>
          <circle cx="18" cy="18" r={R} fill="none" stroke="var(--border)" strokeWidth="2.5" />
          {shown != null && shown > 0 && (
            <circle
              cx="18"
              cy="18"
              r={R}
              fill="none"
              stroke="currentColor"
              strokeWidth="2.5"
              strokeDasharray={`${Math.max(shown, 1)} ${100 - Math.max(shown, 1)}`}
              className={tone}
            />
          )}
        </svg>
        <span className={cn("absolute font-mono text-[10px] tabular-nums", tone)}>
          {shown == null ? "—" : `${Math.round(shown)}%`}
        </span>
      </span>
      <span className="text-[9px] tracking-[0.12em] text-subtle-foreground uppercase">{label}</span>
    </span>
  );
}
