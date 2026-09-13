import { findEngine } from "./engines";
import { internalHost } from "./references";
import type { Manifest } from "./schema";
import { DEFAULT_PORT, parseSize } from "./values";

export const SCHEMA_VERSION = 1;

export type NormalizedLimits = { cpu: number | null; memory_bytes: number | null };

export type NormalizedSource =
  | { type: "image"; image: string; tag: string | null }
  | {
      type: "dockerfile" | "railpack";
      repo: string;
      ref: string | null;
      dockerfile: string | null;
    };

export type NormalizedApp = {
  key: string;
  name: string;
  source: NormalizedSource;
  port: number | null;
  health: string | null;
  domains: { host: string; port: number }[];
  attach: { kind: "database" | "store"; key: string; bucket: string | null; prefix: string }[];
  env: Record<string, string>;
  limits: NormalizedLimits | null;
  scale: number | null;
  spread: boolean;
  autoscale: { min: number; max: number; cpu: number } | null;
  internal_host: string;
};

export type NormalizedManifest = {
  schema_version: typeof SCHEMA_VERSION;
  min_cubeship: string | null;
  project: string;
  environment: string;
  inputs: Manifest["inputs"];
  databases: {
    key: string;
    name: string;
    engine: string;
    version: string | null;
    username: string | null;
    database: string | null;
    expose: number | null;
    limits: NormalizedLimits | null;
    port: number | null;
  }[];
  stores: {
    key: string;
    name: string;
    version: string | null;
    buckets: string[];
    limits: NormalizedLimits | null;
  }[];
  apps: NormalizedApp[];
};

function limitsOf(
  value: { cpu?: number; memory?: string | number } | undefined,
): NormalizedLimits | null {
  if (!value) return null;
  const memory = value.memory === undefined ? null : (parseSize(value.memory) ?? null);
  return { cpu: value.cpu ?? null, memory_bytes: memory };
}

function sorted(env: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(env).sort(([a], [b]) => a.localeCompare(b)));
}

export function normalize(manifest: Manifest): NormalizedManifest {
  return {
    schema_version: SCHEMA_VERSION,
    min_cubeship: manifest.minCubeship ?? null,
    project: manifest.project,
    environment: manifest.environment,
    inputs: manifest.inputs,
    databases: manifest.databases.map((database) => ({
      key: database.key,
      name: database.name ?? database.key,
      engine: database.engine,
      version: database.version ?? null,
      username: database.username ?? null,
      database: database.database ?? null,
      expose: database.expose ?? null,
      limits: limitsOf(database.limits),
      port: findEngine(database.engine)?.port ?? null,
    })),
    stores: manifest.stores.map((store) => ({
      key: store.key,
      name: store.name ?? store.key,
      version: store.version ?? null,
      buckets: store.buckets,
      limits: limitsOf(store.limits),
    })),
    apps: manifest.apps.map((app) => {
      const name = app.name ?? app.key;
      const source: NormalizedSource = app.image
        ? { type: "image", image: app.image, tag: app.tag ?? null }
        : {
            type: app.build ?? "dockerfile",
            repo: app.repo ?? "",
            ref: app.ref ?? null,
            dockerfile: app.dockerfile ?? null,
          };

      return {
        key: app.key,
        name,
        source,
        port: app.port ?? null,
        health: app.health ?? null,
        domains: app.domains.map((entry) => ({
          host: entry.host,
          port: entry.port ?? app.port ?? DEFAULT_PORT,
        })),
        attach: app.attach.map((entry) => ({
          kind: entry.database ? ("database" as const) : ("store" as const),
          key: (entry.database ?? entry.store) as string,
          bucket: entry.bucket ?? null,
          prefix: entry.prefix,
        })),
        env: sorted(app.env),
        limits: limitsOf(app.limits),
        scale: app.scale ?? null,
        spread: app.spread ?? false,
        autoscale: app.autoscale
          ? { min: app.autoscale.min ?? 1, max: app.autoscale.max, cpu: app.autoscale.cpu }
          : null,
        internal_host: internalHost(manifest.project, manifest.environment, name),
      };
    }),
  };
}
