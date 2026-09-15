"use client";

import { queryOptions } from "@tanstack/react-query";
import {
  type App,
  api,
  type ClusterServer,
  type ContainerUsage,
  type Datastore,
  type InstanceSeries,
  type MetricWindow,
  type ObjectStore,
  type Project,
} from "@/lib/api";

// Cached within the signed-in shell. Revisit immediately paints the last
// inventory, then revalidates so a mutation on another screen is reflected.
export const projectsQuery = queryOptions({
  queryKey: ["projects"],
  queryFn: () => api.get<Project[]>("/projects"),
  staleTime: 0,
});
export const appsQuery = queryOptions({
  queryKey: ["apps"],
  queryFn: () => api.get<App[]>("/apps"),
  staleTime: 0,
});
export const datastoresQuery = queryOptions({
  queryKey: ["datastores"],
  queryFn: () => api.get<Datastore[]>("/datastores"),
  staleTime: 0,
});
export const storesQuery = queryOptions({
  queryKey: ["objectstores"],
  queryFn: () => api.get<ObjectStore[]>("/objectstores"),
  staleTime: 0,
});

// Overview and resource cards share the same readings and refresh interval.
export const containersQuery = queryOptions({
  queryKey: ["instance-containers"],
  queryFn: () => api.get<ContainerUsage[]>("/instance/containers"),
  staleTime: 10_000,
  refetchInterval: 30_000,
});
export const instanceMetricsQuery = (window: MetricWindow, server = "control-plane") =>
  queryOptions({
    queryKey: ["instance-metrics", window, server],
    queryFn: ({ signal }) =>
      api.get<InstanceSeries>(
        `/instance/metrics?window=${window}&server=${encodeURIComponent(server)}`,
        { signal },
      ),
    staleTime: 10_000,
    refetchInterval: 30_000,
  });

export const machinesQuery = queryOptions({
  queryKey: ["machines"],
  queryFn: ({ signal }) => api.get<ClusterServer[]>("/nodes", { signal }),
  staleTime: 10_000,
  refetchInterval: 30_000,
});
