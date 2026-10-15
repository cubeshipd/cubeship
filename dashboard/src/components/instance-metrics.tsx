"use client";

import { useQuery } from "@tanstack/react-query";
import { cn } from "cn";
import { useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { SectionHeader } from "@/components/section-header";
import { TimeSeries } from "@/components/time-series";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { formatBytes, formatCPU, METRIC_WINDOWS, type MetricWindow } from "@/lib/api";
import { instanceMetricsQuery } from "@/lib/dashboard-queries";
import { message } from "@/lib/errors";

// Matched to the daemon's own sampling interval rather than made
// faster: asking twice as often as there is anything new to say is two
// requests for one point.

// What the box is doing — the machine, not one container on it.
//
// Its own component rather than MetricsSection with a flag, because it
// is not the same series: a machine has a disk filling up and a wire
// moving bytes, neither of which a container has, and its CPU is a
// share of the whole box where a container's is a share of one core.
// One component drawing both would be a component with two meanings
// for its most-read number.
export function InstanceMetrics({ server = "control-plane" }: { server?: string }) {
  const [window, setWindow] = useState<MetricWindow>("1h");
  const query = useQuery(instanceMetricsQuery(window, server));
  const series = query.data;
  const error = query.error ? message(query.error) : null;

  const samples = series?.samples ?? [];
  const missing = series?.unavailable ?? {};
  const latest = samples.at(-1);
  const empty = "Nothing sampled yet — the first reading lands within a minute.";

  // The interfaces are named under the two network charts rather than
  // in a tooltip. A figure somebody thinks is too low is one they can
  // check against `ip -s link` on the box, and only if they know which
  // interfaces it added up.
  const interfaces = series?.interfaces?.join(", ");

  return (
    <>
      <SectionHeader
        title={`Monitoring · ${server}`}
        sub={
          series
            ? `${series.cores || "?"} cores, ${formatBytes(series.memory_total_bytes)} of memory, and ${formatBytes(series.disk_total_bytes)} of disk at ${series.disk_path}. Sampled every 30 seconds and kept for a day.`
            : "Sampled from the machine's own counters every 30 seconds and kept for a day."
        }
        actions={
          <div className="flex items-center gap-1">
            {METRIC_WINDOWS.map((w) => (
              <Button
                key={w}
                type="button"
                variant="ghost"
                size="xs"
                aria-pressed={w === window}
                onClick={() => setWindow(w)}
                className={cn("font-mono", w === window && "bg-secondary text-foreground")}
              >
                {w}
              </Button>
            ))}
          </div>
        }
      />

      <ErrorAlert error={error} />
      {series?.sampled_at && Date.now() - Date.parse(series.sampled_at) > 120_000 && (
        <Notice tone="warning">
          No recent samples. The latest reading shown is from{" "}
          {new Date(series.sampled_at).toLocaleString()}; these charts show recorded history.
        </Notice>
      )}

      <div className="mb-4 grid gap-4 lg:grid-cols-2">
        <Chart
          label="CPU · 100% is the whole machine"
          unavailable={missing.cpu}
          points={samples.map((s) => ({ at: s.at, value: s.cpu_percent }))}
          format={formatCPU}
          loading={query.isPending}
          empty={empty}
        />

        <Chart
          label={
            series?.memory_total_bytes
              ? `Memory · of ${formatBytes(series.memory_total_bytes)}`
              : "Memory"
          }
          unavailable={missing.memory}
          points={samples.map((s) => ({ at: s.at, value: s.memory_bytes }))}
          format={formatBytes}
          // Reported beside the peak rather than used as the top of the
          // scale — a box at 2 of 8 GiB would be a flat line along the
          // bottom. See TimeSeries.
          ceiling={series?.memory_total_bytes || undefined}
          accent="var(--magenta)"
          loading={query.isPending}
          empty={empty}
        />

        {/* In and out are two charts rather than two lines on one. The
            question is almost never "which is bigger" — it is "is this
            one a spike", and two axes that each scale to their own
            traffic answer that where a shared one flattens whichever
            is smaller into the floor.

            When there is no network to chart they collapse back into
            one: the same sentence printed twice side by side reads as
            two problems. */}
        {missing.network ? (
          <div className="lg:col-span-2">
            <Chart
              label="Network"
              unavailable={missing.network}
              points={[]}
              format={perSecond}
              loading={query.isPending}
              empty={empty}
            />
          </div>
        ) : (
          <>
            <Chart
              label={interfaces ? `Network in · ${interfaces}` : "Network in"}
              points={rates(samples, "rx_bytes_per_sec")}
              format={perSecond}
              loading={query.isPending}
              empty={empty}
            />

            <Chart
              label={interfaces ? `Network out · ${interfaces}` : "Network out"}
              points={rates(samples, "tx_bytes_per_sec")}
              format={perSecond}
              accent="var(--magenta)"
              loading={query.isPending}
              empty={empty}
            />
          </>
        )}

        {/* Full width and last: it is the slowest line on the screen,
            and the thing worth seeing in it is a slope over a day
            rather than a value at a moment. */}
        <div className="lg:col-span-2">
          <Chart
            label={
              latest && series?.disk_total_bytes
                ? `Disk · ${formatBytes(latest.disk_bytes)} of ${formatBytes(series.disk_total_bytes)} used at ${series.disk_path}`
                : "Disk"
            }
            unavailable={missing.disk}
            points={samples.map((s) => ({ at: s.at, value: s.disk_bytes }))}
            format={formatBytes}
            ceiling={series?.disk_total_bytes || undefined}
            loading={query.isPending}
            empty={empty}
          />
        </div>
      </div>
    </>
  );
}

// One labelled chart, or the sentence saying why there is none.
//
// A measurement this daemon cannot take is a note in the chart's place
// rather than a missing card: a gap where a chart was is a bug report,
// and the reason here is usually an instruction.
function Chart({
  label,
  unavailable,
  points,
  format,
  ceiling,
  accent,
  empty,
  loading,
}: {
  label: string;
  unavailable?: string;
  points: { at: string; value: number }[];
  format: (value: number) => string;
  ceiling?: number;
  accent?: string;
  empty: string;
  loading?: boolean;
}) {
  return (
    <Card>
      <CardContent>
        <div className="dashboard-chart-label">{label}</div>
        {unavailable ? (
          <Notice>{unavailable}</Notice>
        ) : (
          <TimeSeries
            points={points}
            format={format}
            ceiling={ceiling}
            accent={accent}
            loading={loading}
            empty={empty}
          />
        )}
      </CardContent>
    </Card>
  );
}

// A rate is absent on the samples taken before there was anything to
// take a difference against. Those points are left out rather than
// drawn as zero — a zero is a reading, and this is the absence of one.
function rates(
  samples: { at: string; rx_bytes_per_sec?: number; tx_bytes_per_sec?: number }[],
  field: "rx_bytes_per_sec" | "tx_bytes_per_sec",
) {
  const out: { at: string; value: number }[] = [];
  for (const s of samples) {
    const value = s[field];
    if (value !== undefined && value !== null) out.push({ at: s.at, value });
  }
  return out;
}

// Bytes a second, in the same binary units as everything else here.
function perSecond(value: number): string {
  return `${formatBytes(value)}/s`;
}
