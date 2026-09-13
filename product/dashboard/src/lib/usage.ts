"use client";

import { useEffect, useState } from "react";
import { type AppLimits, api, type ContainerUsage, type InstanceSeries } from "@/lib/api";

// The daemon samples every 30 seconds, so anything faster is two
// requests for one reading.
const REFRESH_MS = 30_000;

export type Machine = { cores: number; memory_total_bytes: number };

// The newest reading of every container of one kind, by name, and the
// machine they are shares of. One request for a whole grid rather than
// one per card.
export function useContainerUsage(kind: ContainerUsage["kind"]) {
  const [usage, setUsage] = useState<Map<string, ContainerUsage> | null>(null);
  const [machine, setMachine] = useState<Machine | null>(null);

  useEffect(() => {
    const load = () =>
      api
        .get<ContainerUsage[]>("/instance/containers")
        .then((all) =>
          setUsage(new Map(all.filter((u) => u.kind === kind).map((u) => [u.name, u]))),
        )
        .catch(() => setUsage(new Map()));
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => clearInterval(timer);
  }, [kind]);

  useEffect(() => {
    api
      .get<InstanceSeries>("/instance/metrics?window=1h")
      .then((s) => setMachine({ cores: s.cores, memory_total_bytes: s.memory_total_bytes }))
      .catch(() => setMachine(null));
  }, []);

  return { usage, machine };
}

// What a container's reading is a share of.
//
// CPU against its own limit when it has one, else every core of the
// machine: the reading is percent of one core, so a database using two
// cores of eight is 25% here, not 200%. Memory against the cgroup's
// ceiling, which the daemon already reports as the host's memory for a
// container with no limit.
export function usageShares(
  u: ContainerUsage | undefined,
  limits: AppLimits | undefined,
  machine: Machine | null,
): { cpu?: number; memory?: number } {
  if (!u) return {};
  const cpuCeiling = limits?.cpu ? limits.cpu * 100 : machine ? machine.cores * 100 : 0;
  const memoryCeiling =
    u.memory_limit_bytes || limits?.memory_bytes || machine?.memory_total_bytes || 0;
  return {
    cpu: cpuCeiling > 0 ? (u.cpu_percent / cpuCeiling) * 100 : undefined,
    memory: memoryCeiling > 0 ? (u.memory_bytes / memoryCeiling) * 100 : undefined,
  };
}
