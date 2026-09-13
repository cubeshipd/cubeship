// The normalized manifest hosted/discovery stores, as written by
// product/template/normalize.go. Only types: the site validates nothing,
// it shows what the validator already accepted.

export type NormalizedLimits = { cpu: number | null; memory_bytes: number | null };

export type NormalizedSource =
  | { type: "image"; image: string; tag: string | null }
  | {
      type: "dockerfile" | "railpack";
      repo: string;
      ref: string | null;
      dockerfile: string | null;
    };

export type NormalizedInput = {
  key: string;
  type: "domain" | "text" | "number" | "choice" | "secret" | "store";
  label: string;
  help?: string;
  required: boolean;
  default?: string | number;
  pattern?: string;
  min?: number;
  max?: number;
  options?: string[];
  generate?: number;
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
  schema_version: 1;
  min_cubeship: string | null;
  project: string;
  environment: string;
  inputs: NormalizedInput[];
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
