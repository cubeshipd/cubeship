import { z } from "zod";
import type { Diagnostic, Locate } from "./diagnostics";
import { envKeyPattern, slugPattern, tagPattern } from "./values";

const slug = z.string().regex(slugPattern, "lowercase letters, digits and dashes");
const key = z
  .string()
  .regex(/^[a-zA-Z][a-zA-Z0-9_-]*$/, "a letter, then letters, digits, dash or underscore");

// A YAML scalar may arrive as a number or a boolean; a container's
// environment is strings, so it is one here too.
const envValue = z.union([z.string(), z.number(), z.boolean()]).transform(String);

const limits = z.strictObject({
  cpu: z.number().optional(),
  memory: z.union([z.string(), z.number()]).optional(),
});

const autoscale = z.strictObject({
  min: z.number().int().optional(),
  max: z.number().int(),
  cpu: z.number(),
});

const inputBase = {
  key,
  label: z.string().min(1),
  help: z.string().optional(),
  required: z.boolean().default(true),
};

const input = z.discriminatedUnion("type", [
  z.strictObject({ ...inputBase, type: z.literal("domain") }),
  z.strictObject({
    ...inputBase,
    type: z.literal("text"),
    default: z.string().optional(),
    pattern: z.string().optional(),
  }),
  z.strictObject({
    ...inputBase,
    type: z.literal("number"),
    default: z.number().optional(),
    min: z.number().optional(),
    max: z.number().optional(),
  }),
  z.strictObject({
    ...inputBase,
    type: z.literal("choice"),
    default: z.string().optional(),
    options: z.array(z.string()).min(2),
  }),
  z.strictObject({
    ...inputBase,
    type: z.literal("secret"),
    generate: z.number().int().optional(),
  }),
  z.strictObject({ ...inputBase, type: z.literal("store") }),
]);

const database = z.strictObject({
  key,
  name: slug.optional(),
  engine: z.string(),
  version: z.string().optional(),
  username: z.string().optional(),
  database: z.string().optional(),
  // null is "internal only", 0 is "pick a port", N is that port.
  expose: z.number().int().nullable().optional(),
  limits: limits.optional(),
});

const store = z.strictObject({
  key,
  name: slug.optional(),
  version: z.string().optional(),
  buckets: z.array(z.string()).default([]),
  limits: limits.optional(),
});

const domain = z.strictObject({
  host: z.string().min(1),
  port: z.number().int().min(1).max(65535).optional(),
});

const attach = z.strictObject({
  database: key.optional(),
  store: key.optional(),
  bucket: z.string().optional(),
  prefix: z.string().default(""),
});

const app = z.strictObject({
  key,
  name: slug.optional(),
  image: z.string().optional(),
  tag: z.string().regex(tagPattern).optional(),
  repo: z.string().optional(),
  ref: z.string().optional(),
  build: z.enum(["dockerfile", "railpack"]).optional(),
  dockerfile: z.string().optional(),
  port: z.number().int().min(1).max(65535).optional(),
  health: z.string().optional(),
  domains: z.array(domain).default([]),
  attach: z.array(attach).default([]),
  env: z.record(z.string().regex(envKeyPattern), envValue).default({}),
  limits: limits.optional(),
  scale: z.number().int().min(1).optional(),
  spread: z.boolean().optional(),
  autoscale: autoscale.optional(),
});

export const manifestSchema = z.strictObject({
  version: z.literal(1),
  minCubeship: z.string().optional(),
  project: slug,
  environment: slug.default("production"),
  inputs: z.array(input).default([]),
  databases: z.array(database).default([]),
  stores: z.array(store).default([]),
  apps: z.array(app).min(1, "a template creates at least one app"),
});

export type Manifest = z.output<typeof manifestSchema>;

// Zod names no valid keys in an unrecognized-key issue, so the sets live
// here, chosen by the shape of the path the issue came from.
const keySets: Record<string, string[]> = {
  manifest: [
    "version",
    "minCubeship",
    "project",
    "environment",
    "inputs",
    "databases",
    "stores",
    "apps",
  ],
  input: [
    "key",
    "type",
    "label",
    "help",
    "required",
    "default",
    "pattern",
    "min",
    "max",
    "options",
    "generate",
  ],
  database: ["key", "name", "engine", "version", "username", "database", "expose", "limits"],
  store: ["key", "name", "version", "buckets", "limits"],
  app: [
    "key",
    "name",
    "image",
    "tag",
    "repo",
    "ref",
    "build",
    "dockerfile",
    "port",
    "health",
    "domains",
    "attach",
    "env",
    "limits",
    "scale",
    "spread",
    "autoscale",
  ],
  domain: ["host", "port"],
  attach: ["database", "store", "bucket", "prefix"],
  limits: ["cpu", "memory"],
  autoscale: ["min", "max", "cpu"],
};

export function keysAt(path: (string | number)[]): string[] {
  const shape = path.filter((p) => typeof p === "string").join(".");
  if (path.length === 0) return keySets.manifest;
  if (shape === "inputs") return keySets.input;
  if (shape === "databases") return keySets.database;
  if (shape === "stores") return keySets.store;
  if (shape === "apps") return keySets.app;
  if (shape === "apps.domains") return keySets.domain;
  if (shape === "apps.attach") return keySets.attach;
  if (shape.endsWith("limits")) return keySets.limits;
  if (shape.endsWith("autoscale")) return keySets.autoscale;
  return [];
}

function distance(a: string, b: string): number {
  const rows = Array.from({ length: a.length + 1 }, (_, i) => [i, ...Array(b.length).fill(0)]);
  for (let j = 0; j <= b.length; j++) rows[0][j] = j;
  for (let i = 1; i <= a.length; i++) {
    for (let j = 1; j <= b.length; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      rows[i][j] = Math.min(rows[i - 1][j] + 1, rows[i][j - 1] + 1, rows[i - 1][j - 1] + cost);
    }
  }
  return rows[a.length][b.length];
}

function nearest(key: string, candidates: string[]): string | undefined {
  const lower = key.toLowerCase();
  // "healthcheck" for "health" is not a typo, it is the old name; edit
  // distance alone would place it too far away to suggest.
  const contained = candidates.find((candidate) => {
    const c = candidate.toLowerCase();
    return lower.includes(c) || c.includes(lower);
  });
  if (contained) return contained;

  const ranked = candidates
    .map((candidate) => ({ candidate, d: distance(lower, candidate.toLowerCase()) }))
    .sort((a, b) => a.d - b.d);
  const best = ranked[0];
  return best && best.d <= 2 ? best.candidate : undefined;
}

export function fromZod(error: z.ZodError, locate: Locate): Diagnostic[] {
  return error.issues.flatMap((issue): Diagnostic[] => {
    const path = issue.path as (string | number)[];

    if (issue.code === "unrecognized_keys") {
      return issue.keys.map((unknown) => {
        const suggestion = nearest(unknown, keysAt(path));
        return {
          severity: "error" as const,
          code: "schema.unknown-key",
          message: `unknown key "${unknown}"`,
          path: [...path, unknown],
          range: locate([...path, unknown], "key") ?? locate(path),
          hint: suggestion ? `did you mean "${suggestion}"?` : undefined,
        };
      });
    }

    return [
      {
        severity: "error",
        code: `schema.${issue.code}`,
        message: issue.message,
        path,
        range: locate(path, "key") ?? locate(path.slice(0, -1)),
      },
    ];
  });
}
