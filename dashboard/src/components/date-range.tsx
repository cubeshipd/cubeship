"use client";

import { cn } from "cn";
import { CalendarIcon, ChevronDownIcon, ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { useState } from "react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

// A span of time: from, inclusive, to, exclusive. Either may be open.
// `label` is set by a preset, whose name says it better than two dates.
export type DateRange = { from?: Date; to?: Date; label?: string };

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

const PRESETS: { label: string; span: number }[] = [
  { label: "Last hour", span: HOUR },
  { label: "Last 24 hours", span: DAY },
  { label: "Last 7 days", span: 7 * DAY },
  { label: "Last 30 days", span: 30 * DAY },
  { label: "Last 90 days", span: 90 * DAY },
];

// In the browser's language, like the month above them and every date on
// the page: 5 Jan 1970 was a Monday.
const WEEKDAYS = Array.from({ length: 7 }, (_, i) =>
  new Date(1970, 0, 5 + i).toLocaleDateString(undefined, { weekday: "narrow" }),
);

function startOfDay(d: Date) {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function addDays(d: Date, n: number) {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);
}

function sameDay(a?: Date, b?: Date) {
  return !!a && !!b && a.toDateString() === b.toDateString();
}

function short(d: Date) {
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

// What the field says: the preset, both days, one day, or "Any time".
function describe(range: DateRange, empty: string) {
  if (range.label) return range.label;
  const { from, to } = range;
  if (!from && !to) return empty;
  const last = to ? addDays(to, -1) : undefined;
  if (from && last && sameDay(from, last)) return short(from);
  // One month written once, so the field fits beside the other filters.
  if (
    from &&
    last &&
    from.getMonth() === last.getMonth() &&
    from.getFullYear() === last.getFullYear()
  ) {
    return `${from.getDate()}–${short(last)}`;
  }
  return `${from ? short(from) : "…"} – ${last ? short(last) : "now"}`;
}

// A trigger the height of a filter field, and a popup with presets beside
// one month. Days are picked as a pair: the first click starts the range,
// the second ends it — in either order — and the day it ends on counts.
export function DateRangePicker({
  value,
  onChange,
  empty = "Any time",
  className,
}: {
  value: DateRange;
  onChange: (range: DateRange) => void;
  empty?: string;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [month, setMonth] = useState(() => startOfDay(value.from ?? new Date()));
  // The first day of a pick in progress.
  const [anchor, setAnchor] = useState<Date | null>(null);
  const [hover, setHover] = useState<Date | null>(null);

  const today = startOfDay(new Date());
  const first = new Date(month.getFullYear(), month.getMonth(), 1);
  // Monday-first: getDay() is Sunday-first.
  const lead = (first.getDay() + 6) % 7;
  const days = Array.from({ length: 42 }, (_, i) => addDays(first, i - lead));

  // What is lit: the pick in progress against the pointer, or the value.
  let lo: Date | undefined;
  let hi: Date | undefined;
  if (anchor) {
    const other = hover ?? anchor;
    [lo, hi] = anchor <= other ? [anchor, other] : [other, anchor];
  } else if (value.from || value.to) {
    lo = value.from ? startOfDay(value.from) : undefined;
    hi = value.to ? addDays(value.to, -1) : today;
  }

  function pick(day: Date) {
    if (!anchor) {
      setAnchor(day);
      return;
    }
    const [a, b] = anchor <= day ? [anchor, day] : [day, anchor];
    onChange({ from: a, to: addDays(b, 1) });
    setAnchor(null);
    setOpen(false);
  }

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        setAnchor(null);
        if (next) setMonth(startOfDay(value.from ?? new Date()));
      }}
    >
      <PopoverTrigger
        render={
          <button
            type="button"
            data-slot="select-trigger"
            className={cn(
              "flex h-10 cursor-pointer items-center gap-2 border border-border px-3 text-left text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40",
              className,
            )}
          >
            <CalendarIcon className="size-4 shrink-0 text-primary" aria-hidden="true" />
            <span className="min-w-0 flex-1 truncate">{describe(value, empty)}</span>
            <ChevronDownIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          </button>
        }
      />
      {/* The presets sit above the calendar on a phone: side by side the two
          are wider than the screen. The switch is at 761px, not at `sm`, because
          up to 760px the days grow to 44px (dashboard.css), and seven of them
          do not fit in the 256px column the desktop calendar has. */}
      <PopoverContent
        align="end"
        className="flex max-h-(--available-height) max-w-[calc(100vw-1rem)] flex-col overflow-y-auto p-0 min-[761px]:max-w-none min-[761px]:flex-row"
      >
        <div className="flex w-full flex-row flex-wrap border-border border-b p-1 min-[761px]:w-36 min-[761px]:flex-col min-[761px]:border-r min-[761px]:border-b-0">
          {PRESETS.map((p) => (
            <button
              key={p.label}
              type="button"
              onClick={() => {
                onChange({ from: new Date(Date.now() - p.span), label: p.label });
                setOpen(false);
              }}
              className={cn(
                "px-3 py-2 text-left text-xs hover:bg-secondary",
                value.label === p.label && "bg-secondary text-primary",
              )}
            >
              {p.label}
            </button>
          ))}
          <div className="my-1 hidden border-border border-t min-[761px]:block" />
          <button
            type="button"
            onClick={() => {
              onChange({});
              setOpen(false);
            }}
            className={cn(
              "px-3 py-2 text-left text-xs hover:bg-secondary",
              !value.from && !value.to && "bg-secondary text-primary",
            )}
          >
            {empty}
          </button>
        </div>

        <div className="w-full p-3 min-[761px]:w-64">
          <div className="mb-2 flex items-center justify-between">
            <button
              type="button"
              aria-label="Previous month"
              onClick={() => setMonth(new Date(month.getFullYear(), month.getMonth() - 1, 1))}
              className="flex size-7 items-center justify-center text-muted-foreground hover:bg-secondary hover:text-foreground"
            >
              <ChevronLeftIcon className="size-4" />
            </button>
            <span className="font-mono text-xs uppercase tracking-wide">
              {month.toLocaleDateString(undefined, { month: "long", year: "numeric" })}
            </span>
            <button
              type="button"
              aria-label="Next month"
              disabled={
                first.getFullYear() === today.getFullYear() && first.getMonth() === today.getMonth()
              }
              onClick={() => setMonth(new Date(month.getFullYear(), month.getMonth() + 1, 1))}
              className="flex size-7 items-center justify-center text-muted-foreground hover:bg-secondary hover:text-foreground disabled:opacity-30 disabled:hover:bg-transparent"
            >
              <ChevronRightIcon className="size-4" />
            </button>
          </div>

          <div className="grid grid-cols-7 text-center">
            {WEEKDAYS.map((d, i) => (
              // By position: a narrow weekday is one letter, and two are S.
              // biome-ignore lint/suspicious/noArrayIndexKey: seven fixed columns that never reorder
              <span key={i} className="py-1 font-mono text-[10px] text-subtle-foreground">
                {d}
              </span>
            ))}
            {days.map((day) => {
              const outside = day.getMonth() !== month.getMonth();
              // Nothing was recorded tomorrow.
              const future = day > today;
              const inRange = lo && hi && day >= lo && day <= hi;
              const edge = sameDay(day, lo) || sameDay(day, hi);
              return (
                <button
                  key={day.toISOString()}
                  type="button"
                  disabled={future}
                  onClick={() => pick(day)}
                  onMouseEnter={() => anchor && setHover(day)}
                  className={cn(
                    "h-8 font-mono text-xs transition-colors",
                    outside ? "text-subtle-foreground" : "text-foreground",
                    !inRange && !future && "hover:bg-secondary",
                    inRange && "bg-primary/15",
                    edge && "bg-primary text-primary-foreground",
                    sameDay(day, today) && !edge && "text-primary",
                    future && "cursor-not-allowed opacity-30",
                  )}
                >
                  {day.getDate()}
                </button>
              );
            })}
          </div>

          <p className="mt-2 h-4 text-[11px] text-muted-foreground">
            {anchor ? "Pick the last day." : "Pick the first day."}
          </p>
        </div>
      </PopoverContent>
    </Popover>
  );
}
