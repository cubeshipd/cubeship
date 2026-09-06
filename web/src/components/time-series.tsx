"use client";

import { cn } from "cn";
import { useId, useMemo, useState } from "react";

// One line over time, drawn by hand rather than by a charting library.
//
// A library would be a dependency, a bundle and a theme to fight: this
// needs a line, a fill and a crosshair, in a house style that is 1px
// rules and glow and no shadows. What it costs instead is the two
// things a chart library is actually for, and both are below — a scale
// that survives a flat series, and a hover that reads the point under
// the cursor rather than the nearest pixel.
//
// The viewBox is a fixed grid and the SVG stretches to its container,
// so nothing here has to measure the DOM. `vector-effect` is what keeps
// the line 1px while the box is scaled non-uniformly; without it a wide
// chart draws a hairline and a narrow one draws a rope.
const VIEW_W = 600;
const VIEW_H = 140;

export type Point = { at: string; value: number };

export function TimeSeries({
  points,
  format,
  // ceiling is the headroom this can grow into — a memory limit. It is
  // reported, not drawn against: a container using 200 MiB of a 2 GiB
  // cgroup would be a flat line along the bottom, which is a chart that
  // has given up its only job to answer a question a number answers
  // better. The scale comes from the data; the ceiling is the caption.
  ceiling,
  accent = "var(--primary)",
  empty,
  className,
}: {
  points: Point[];
  format: (value: number) => string;
  ceiling?: number;
  accent?: string;
  empty?: React.ReactNode;
  className?: string;
}) {
  const gradient = useId();
  const [hover, setHover] = useState<number | null>(null);

  const scale = useMemo(() => {
    if (points.length === 0) return null;
    const peak = Math.max(...points.map((p) => p.value));
    // Headroom above the peak, so the highest point is a peak rather
    // than a line touching the top edge — which reads as clipped.
    //
    // Number.EPSILON is the floor: a flat series at zero has no range,
    // and dividing by it puts every point on the same pixel or on none.
    // With it, an idle container draws a line along the bottom.
    const top = Math.max(peak * 1.15, Number.EPSILON);
    return { top, peak };
  }, [points]);

  const geometry = useMemo(() => {
    if (!scale || points.length === 0) return null;
    const step = points.length === 1 ? 0 : VIEW_W / (points.length - 1);
    const coords = points.map((p, i) => ({
      x: points.length === 1 ? VIEW_W / 2 : i * step,
      y: VIEW_H - (p.value / scale.top) * VIEW_H,
    }));
    const line = coords.map((c, i) => `${i === 0 ? "M" : "L"}${c.x},${c.y}`).join(" ");
    const area = `${line} L${coords[coords.length - 1].x},${VIEW_H} L${coords[0].x},${VIEW_H} Z`;
    return { coords, line, area };
  }, [points, scale]);

  if (!geometry || !scale) {
    return (
      <div
        className={cn(
          "flex h-36 items-center justify-center border border-border bg-background text-xs text-muted-foreground",
          className,
        )}
      >
        {empty ?? "No samples yet."}
      </div>
    );
  }

  const active = hover !== null ? points[hover] : points[points.length - 1];
  const activeCoord = geometry.coords[hover ?? points.length - 1];

  return (
    <div className={cn("relative", className)}>
      {/* The reading sits over the chart rather than beside it: it is
          the same fact the line is, and the eye should not have to
          travel between them. */}
      <div className="pointer-events-none absolute top-2 left-3 z-10">
        <div className="font-mono text-lg leading-none text-foreground">{format(active.value)}</div>
        <div className="mt-1 font-mono text-[10px] text-subtle-foreground">
          {hover === null ? "now" : timeOf(active.at)}
        </div>
      </div>
      <div className="pointer-events-none absolute top-2 right-3 z-10 text-right">
        <div className="font-mono text-[10px] text-subtle-foreground">
          peak {format(scale.peak)}
        </div>
        {ceiling ? (
          <div className="font-mono text-[10px] text-subtle-foreground">of {format(ceiling)}</div>
        ) : null}
      </div>

      <svg
        viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
        preserveAspectRatio="none"
        className="h-36 w-full border border-border bg-background"
        role="img"
        aria-label={`Time series, currently ${format(active.value)}`}
        onMouseLeave={() => setHover(null)}
        onMouseMove={(e) => {
          const box = e.currentTarget.getBoundingClientRect();
          const ratio = (e.clientX - box.left) / box.width;
          const index = Math.round(ratio * (points.length - 1));
          setHover(Math.min(points.length - 1, Math.max(0, index)));
        }}
      >
        <defs>
          <linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={accent} stopOpacity="0.28" />
            <stop offset="100%" stopColor={accent} stopOpacity="0" />
          </linearGradient>
        </defs>

        {/* Quarters rather than a labelled axis. The numbers that matter
            are the current one and the peak, and both are already
            written above — these are only there to give the line
            something to be high or low against. */}
        {[0.25, 0.5, 0.75].map((fraction) => (
          <line
            key={fraction}
            x1="0"
            x2={VIEW_W}
            y1={VIEW_H * fraction}
            y2={VIEW_H * fraction}
            stroke="var(--border)"
            strokeWidth="1"
            vectorEffect="non-scaling-stroke"
          />
        ))}

        <path d={geometry.area} fill={`url(#${gradient})`} />
        <path
          d={geometry.line}
          fill="none"
          stroke={accent}
          strokeWidth="1.5"
          strokeLinejoin="round"
          vectorEffect="non-scaling-stroke"
        />

        {hover !== null && (
          <line
            x1={activeCoord.x}
            x2={activeCoord.x}
            y1="0"
            y2={VIEW_H}
            stroke={accent}
            strokeOpacity="0.5"
            strokeWidth="1"
            vectorEffect="non-scaling-stroke"
          />
        )}
        {/* The dot is drawn in a nested SVG of its own so the circle
            keeps its shape: everything above is scaled non-uniformly by
            preserveAspectRatio, and a circle in that becomes an
            ellipse. */}
        <svg
          aria-hidden="true"
          x={`${(activeCoord.x / VIEW_W) * 100}%`}
          y={`${(activeCoord.y / VIEW_H) * 100}%`}
          overflow="visible"
        >
          <circle r="3" fill={accent} />
        </svg>
      </svg>

      <div className="mt-1.5 flex justify-between font-mono text-[10px] text-subtle-foreground">
        <span>{timeOf(points[0].at)}</span>
        <span>{timeOf(points[points.length - 1].at)}</span>
      </div>
    </div>
  );
}

// Times only. A window is at most a day, so the date is the same on
// both ends and printing it twice says nothing.
function timeOf(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}
