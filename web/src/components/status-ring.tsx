import { cn } from "cn";
import { statusTone } from "@/components/status-badge";

// A thin donut: how many things there are in the middle, and what state
// they are in around the edge.
//
// **It replaces a row of dots with a number beside each.** That was a
// legend you had to read — three lamps and three counts, four glyphs
// wide — to answer a question that is nearly always "are they all
// fine". A ring answers it with its shape: whole and green is
// everything, and a bite out of it is the thing to open.
//
// The colours come from `statusTone`, the one place a state becomes a
// colour, so a ring cannot disagree with the badge on the screen behind
// it. They are Tailwind text classes, which is why every arc strokes
// `currentColor`.
//
// **The radius is not a round number on purpose.** At r = 15.9155 the
// circumference is 100, so a share of the ring is that share of the
// dash array and the arithmetic is the percentage itself.
const R = 15.9155;

// The order arcs are drawn in, worst last so the thing worth seeing is
// never the one painted over at a seam. Anything unknown sorts after
// all of them, for the reason `statusTone` has no default colour: a
// state nobody here has named should look unfamiliar.
const ORDER = ["text-success", "text-muted-foreground", "text-warning", "text-destructive"];

export function StatusRing({
  values,
  className,
  label,
}: {
  // One entry per thing — an app, a replica. Counted here rather than
  // by the caller, so every ring in the product groups them the same
  // way.
  values: string[];
  className?: string;
  // What a screen reader is told, since the ring is a picture of a
  // number and the number alone is not the point.
  label?: string;
}) {
  const counts = new Map<string, number>();
  for (const v of values) counts.set(v, (counts.get(v) ?? 0) + 1);

  const arcs = [...counts]
    .map(([status, n]) => ({ status, n, tone: statusTone(status).text }))
    .sort((a, b) => {
      const ai = ORDER.indexOf(a.tone);
      const bi = ORDER.indexOf(b.tone);
      return (ai < 0 ? ORDER.length : ai) - (bi < 0 ? ORDER.length : bi);
    });

  let offset = 0;
  const total = values.length;

  return (
    <span
      className={cn("relative inline-flex size-11 shrink-0 items-center justify-center", className)}
      role="img"
      aria-label={label}
    >
      <svg viewBox="0 0 36 36" className="size-full -rotate-90">
        <title>{label ?? `${total}`}</title>
        {/* The track. It is the whole ring for something with nothing
            in it, which is a state rather than an absence: a project
            with no apps is a project you have not deployed to yet. */}
        <circle cx="18" cy="18" r={R} fill="none" stroke="var(--border)" strokeWidth="1.75" />
        {arcs.map((arc) => {
          const share = (arc.n / total) * 100;
          // A hair off each end, so two arcs that meet read as two
          // rather than as one longer one. Not taken off a ring that is
          // all one state — a gap there would be a break saying
          // something is missing.
          const gap = arcs.length > 1 ? 1.5 : 0;
          const dash = Math.max(share - gap, 0.5);
          const at = offset;
          offset += share;
          return (
            <circle
              key={arc.status}
              cx="18"
              cy="18"
              r={R}
              fill="none"
              stroke="currentColor"
              strokeWidth="1.75"
              strokeLinecap="butt"
              strokeDasharray={`${dash} ${100 - dash}`}
              strokeDashoffset={-at}
              className={arc.tone}
              style={{ filter: "drop-shadow(0 0 2px currentColor)" }}
            />
          );
        })}
      </svg>
      <span className="absolute font-mono text-[13px] text-foreground tabular-nums">{total}</span>
    </span>
  );
}
