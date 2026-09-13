"use client";

import { cn } from "cn";
import { useId, useState } from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

// One line over time, in the house style, over Recharts.
//
// It was drawn by hand first, and the hand-drawn version was fine right
// up to the moment a second kind of chart was wanted: a bar, a second
// series, a legend, a brush. Each of those is a rewrite of the geometry
// here and a new set of edge cases nobody has hit yet — an axis that
// has to pick its own ticks, a tooltip that has to survive the edge of
// the container. Recharts already has all of it, and what it takes in
// return is a theme, which is one file rather than one component.
//
// **The look did not move.** 1px rules, a gradient fill under the line,
// a crosshair on hover, the current reading over the top left and the
// peak over the top right, and no shadows anywhere. Recharts is asked
// for the geometry and told everything about the paint.
//
// The API is deliberately the small one: points, how to format a value,
// and an optional ceiling. Anything that needs a second series or a
// different mark belongs beside this in `src/components`, sharing the
// theme below rather than restyling a chart per page.

export type Point = { at: string; value: number };

// The palette every chart here draws with, so a second one cannot come
// out a different shade of the same idea.
const GRID = "var(--border)";

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
  // What the reading over the chart shows. Null is "the latest", which
  // is what it says before the cursor is anywhere near it.
  const [hover, setHover] = useState<Point | null>(null);

  if (points.length === 0) {
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

  const peak = Math.max(...points.map((p) => p.value));
  // Headroom above the peak, so the highest point is a peak rather than
  // a line touching the top edge — which reads as clipped. The floor
  // keeps a series that is flat at zero from having no range at all,
  // which would put every point on the same pixel or on none.
  const top = Math.max(peak * 1.15, Number.EPSILON);
  const active = hover ?? points[points.length - 1];

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
        <div className="font-mono text-[10px] text-subtle-foreground">peak {format(peak)}</div>
        {ceiling ? (
          <div className="font-mono text-[10px] text-subtle-foreground">of {format(ceiling)}</div>
        ) : null}
      </div>

      <div className="h-36 w-full border border-border bg-background">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart
            data={points}
            margin={{ top: 0, right: 0, bottom: 0, left: 0 }}
            // Recharts reports which point the cursor is over, which
            // is what the readout above shows. Taken here rather than
            // from the tooltip, so one number stays on screen whether
            // or not a tooltip is what is being looked at.
            onMouseMove={(state) => {
              // The index arrives as a string on some charts and a
              // number on others, so it is coerced rather than
              // narrowed: reading it wrong leaves the number above
              // frozen on "now" while the crosshair moves, which looks
              // like the chart is broken.
              const index = Number(state?.activeTooltipIndex);
              setHover(Number.isInteger(index) ? (points[index] ?? null) : null);
            }}
            onMouseLeave={() => setHover(null)}
          >
            <defs>
              <linearGradient id={gradient} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={accent} stopOpacity={0.28} />
                <stop offset="100%" stopColor={accent} stopOpacity={0} />
              </linearGradient>
            </defs>

            {/* Quarters rather than a labelled axis. The numbers that
                matter are the current one and the peak, and both are
                already written above — these are only there to give the
                line something to be high or low against. */}
            <CartesianGrid
              stroke={GRID}
              strokeWidth={1}
              vertical={false}
              // Quarters of the box, not ticks of the scale. The
              // numbers that matter are the current one and the peak,
              // and both are written above — these only give the line
              // something to be high or low against, and a tick-derived
              // set moves as the data does.
              horizontalCoordinatesGenerator={({ offset }) => {
                const top = offset?.top ?? 0;
                const height = offset?.height ?? 0;
                return [0.25, 0.5, 0.75].map((fraction) => top + height * fraction);
              }}
            />

            {/* Both axes exist to fix the scale, and neither is drawn:
                the chart fills its box edge to edge, which is what lets
                it sit inside a 1px frame rather than floating in
                padding. */}
            <XAxis dataKey="at" hide />
            <YAxis domain={[0, top]} hide />

            <Tooltip
              cursor={{ stroke: accent, strokeOpacity: 0.5, strokeWidth: 1 }}
              // The readout above is the tooltip. A second box floating
              // beside the cursor would say the same number twice, and
              // the house style has no floating panels.
              content={() => null}
              isAnimationActive={false}
            />

            <Area
              type="linear"
              dataKey="value"
              stroke={accent}
              strokeWidth={1.5}
              strokeLinejoin="round"
              fill={`url(#${gradient})`}
              isAnimationActive={false}
              // A dot on the latest point and nowhere else: it is what
              // the readout above is about when the cursor is
              // elsewhere. Every point marked would be a bead curtain.
              dot={({ cx, cy, index, key }) =>
                index === points.length - 1 ? (
                  <circle key={key} cx={cx} cy={cy} r={3} fill={accent} />
                ) : (
                  <g key={key} />
                )
              }
              activeDot={{ r: 3, fill: accent, stroke: "none" }}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>

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
