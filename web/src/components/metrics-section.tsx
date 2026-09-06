"use client";

import { cn } from "cn";
import { useCallback, useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { SectionHeader } from "@/components/page-header";
import { TimeSeries } from "@/components/time-series";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  api,
  formatBytes,
  formatCPU,
  METRIC_WINDOWS,
  type MetricSeries,
  type MetricWindow,
} from "@/lib/api";
import { message } from "@/lib/errors";

// How often the page asks for a fresh series.
//
// Matched to the daemon's own sampling interval rather than made
// faster: asking twice as often as there is anything new to say is two
// requests for one point.
const REFRESH_MS = 30_000;

// What a container is using, over time.
//
// One component for apps and databases both, because it is the same
// question about the same kind of thing — the only difference is the
// address, which is why that is a prop. See internal/metrics on the
// daemon, which is one module for the same reason.
export function MetricsSection({ path, title = "Monitoring" }: { path: string; title?: string }) {
  const [window, setWindow] = useState<MetricWindow>("1h");
  const [series, setSeries] = useState<MetricSeries | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    api
      .get<MetricSeries>(`${path}/metrics?window=${window}`)
      .then((s) => {
        setSeries(s);
        setError(null);
      })
      .catch((e) => setError(message(e)));
  }, [path, window]);

  useEffect(() => {
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => clearInterval(timer);
  }, [load]);

  const samples = series?.samples ?? [];

  // Nothing sampled and nothing to sample are different sentences. One
  // is worth waiting through; the other is worth acting on.
  const empty = series?.collecting
    ? "Nothing sampled yet — the first reading lands within a minute."
    : "There is no container running, so there is nothing to sample.";

  return (
    <>
      <SectionHeader
        title={title}
        sub="Sampled from the container's own counters every 30 seconds and kept for a day."
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

      <div className="mb-4 grid gap-3 lg:grid-cols-2">
        <Card>
          <CardContent>
            <div className="mb-2 text-[11px] tracking-[0.12em] text-muted-foreground uppercase">
              {/* Said here rather than in a tooltip, because it is the
                  one thing about this number that surprises people. */}
              CPU · 100% is one core
            </div>
            <TimeSeries
              points={samples.map((s) => ({ at: s.at, value: s.cpu_percent }))}
              format={formatCPU}
              empty={empty}
            />
          </CardContent>
        </Card>

        <Card>
          <CardContent>
            <div className="mb-2 text-[11px] tracking-[0.12em] text-muted-foreground uppercase">
              Memory
            </div>
            <TimeSeries
              points={samples.map((s) => ({ at: s.at, value: s.memory_bytes }))}
              format={formatBytes}
              // The cgroup's ceiling, reported beside the peak rather
              // than used as the top of the scale — see TimeSeries.
              ceiling={series?.memory_limit_bytes || undefined}
              accent="var(--magenta)"
              empty={empty}
            />
          </CardContent>
        </Card>
      </div>
    </>
  );
}
