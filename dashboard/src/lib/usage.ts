"use client";

import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import type { AppLimits, ContainerUsage } from "@/lib/api";
import { containersQuery, instanceMetricsQuery } from "@/lib/dashboard-queries";

// The daemon samples every 30 seconds, so anything faster is two
// requests for one reading.

export type Machine = { cores: number; memory_total_bytes: number };

// One subject's readings added up. An app on several machines reports
// one reading per replica under the same name.
export type Reading = {
  cpu_percent: number;
  memory_bytes: number;
  memory_limit_bytes: number;
  containers: number;
};

export type Shares = { cpu?: number; memory?: number };

// The newest readings of every container of one kind, by name, and the
// machine they are shares of. One request for a whole grid rather than
// one per card.
export function useContainerUsage(kind: ContainerUsage["kind"]) {
  const containers = useQuery(containersQuery);
  const metrics = useQuery(instanceMetricsQuery("1h"));
  const usage = useMemo(() => {
    if (!containers.data) return null;
    const byName = new Map<string, Reading>();
    for (const u of containers.data) {
      if (u.kind !== kind) continue;
      const r = byName.get(u.name) ?? {
        cpu_percent: 0,
        memory_bytes: 0,
        memory_limit_bytes: 0,
        containers: 0,
      };
      r.cpu_percent += u.cpu_percent;
      r.memory_bytes += u.memory_bytes;
      r.memory_limit_bytes += u.memory_limit_bytes;
      r.containers += 1;
      byName.set(u.name, r);
    }
    return byName;
  }, [containers.data, kind]);
  const machine: Machine | null = metrics.data
    ? { cores: metrics.data.cores, memory_total_bytes: metrics.data.memory_total_bytes }
    : null;
  return { usage, machine };
}

// What a reading is a share of.
//
// CPU against the limit when there is one, else every core of the
// machine: the reading is percent of one core, so two cores busy of
// eight is 25% here, not 200%. Memory against the cgroup's ceiling for
// one container — the daemon reports the host's memory when it has no
// limit — and against limit × replicas, or the machine, for several,
// where adding host-sized ceilings would count the machine twice.
export function usageShares(
  r: Reading | undefined,
  limits: AppLimits | undefined,
  machine: Machine | null,
): Shares {
  if (!r) return {};
  const cpuCeiling = limits?.cpu
    ? limits.cpu * 100 * r.containers
    : machine
      ? machine.cores * 100
      : 0;
  const memoryCeiling =
    r.containers === 1
      ? r.memory_limit_bytes || limits?.memory_bytes || machine?.memory_total_bytes || 0
      : (limits?.memory_bytes ?? 0) * r.containers || machine?.memory_total_bytes || 0;
  return share(r, cpuCeiling, memoryCeiling);
}

// Every app of a project added up, as a share of the machine: a project
// has no limit of its own.
export function projectShares(
  usage: Map<string, Reading> | null,
  project: string,
  machine: Machine | null,
): Shares {
  if (!usage || !machine) return {};
  const total: Reading = { cpu_percent: 0, memory_bytes: 0, memory_limit_bytes: 0, containers: 0 };
  for (const [name, r] of usage) {
    if (!name.startsWith(`${project}/`)) continue;
    total.cpu_percent += r.cpu_percent;
    total.memory_bytes += r.memory_bytes;
    total.containers += r.containers;
  }
  return share(total, machine.cores * 100, machine.memory_total_bytes);
}

function share(r: Reading, cpuCeiling: number, memoryCeiling: number): Shares {
  return {
    cpu: cpuCeiling > 0 ? (r.cpu_percent / cpuCeiling) * 100 : undefined,
    memory: memoryCeiling > 0 ? (r.memory_bytes / memoryCeiling) * 100 : undefined,
  };
}
