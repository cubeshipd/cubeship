# Template Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the community template registry inside `site/`: authors sign in with GitHub, write a template file in a validating editor, publish immutable versions, and anyone can browse, like and comment on them.

**Architecture:** One Next.js app, already deployed as `Dockerfile.site`, gains Postgres through Drizzle, GitHub sign-in, an S3-compatible bucket for photos, and one route-handler API under `/api/v1` that both the browser and future machine consumers use. The template file's schema is a single Zod module that runs in the editor for feedback and on the server as the authority.

**Tech Stack:** Next 16, React 19, TypeScript, Tailwind 4, Drizzle ORM with node-postgres, Zod 4, the `yaml` package for positioned parsing, CodeMirror 6, sharp, `@aws-sdk/client-s3`, Vitest, Biome.

**Spec:** [docs/design/templates.md](../design/templates.md) — read it before starting any task. Every "why" in this plan is argued there.

## Global Constraints

- **Work in `site/`.** No Go changes in this plan. `make check` stays Go-only and must keep passing untouched.
- **Biome is the formatter and the linter.** Double quotes, semicolons always, 2-space indent, line width 100. Run `pnpm run format` before every commit and `pnpm run lint` before every push.
- **`next build` runs with no database.** No module may read `process.env` at import time, and no page may query Postgres during the build. Every page that reads the database is request-rendered.
- **No AI attribution anywhere.** Not in commits, not in code comments, not in documentation. Commit subjects are imperative and say what changes.
- **Comment rule:** a comment says only what the code cannot — the reason, the trap, the unit. The repository's design docs carry the long-form reasoning.
- **Slug shape, copied from the daemon:** `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`. Reserved: `settings` everywhere, plus `engines` for database names and `providers` for store names.
- **Engine catalogue, copied from the daemon:** postgres 18/17/16/15, mysql 8.4/8.0, mariadb 11.4/10.11, redis 7.4/7.2, mongodb 8.0/7.0.
- **Limit floors, copied from the daemon:** CPU at least 0.01 cores, memory at least 6 MiB (6291456 bytes), autoscale max at most 100.
- **Schema URL is permanent:** `/schema/template/v1.json`. API prefix is permanent: `/api/v1`.
- **Two deviations from the spec, already decided.** Migrations run in Next's `instrumentation.ts` rather than a container entrypoint, because `output: "standalone"` traces only what code imports and a loose script would need its own `node_modules`. And there is no `SESSION_SECRET`: session tokens are random and stored hashed, and the OAuth state is compared rather than signed, so the variable would be a secret with no job. Task 10 updates the design doc to match.

---

## File Structure

**The validator, framework-free and unit-tested (Phase 1):**

| File | Responsibility |
| --- | --- |
| `src/lib/template/diagnostics.ts` | the `Diagnostic` type, severities, and the helper that says whether a list blocks a publish |
| `src/lib/template/values.ts` | primitives shared by schema and semantics: slug pattern, reserved words, size parsing, env-key and prefix patterns |
| `src/lib/template/engines.ts` | the engine catalogue and the variable names each engine contributes |
| `src/lib/template/parse.ts` | YAML to a value plus a `locate(path)` that returns a byte range |
| `src/lib/template/schema.ts` | the Zod schema, and Zod issues turned into diagnostics |
| `src/lib/template/references.ts` | the `${...}` grammar, the attributes each kind has, and the internal-host string |
| `src/lib/template/semantics.ts` | every cross-field rule the shape cannot express |
| `src/lib/template/advice.ts` | warnings that never block a publish |
| `src/lib/template/normalize.ts` | the canonical JSON an instance consumes |
| `src/lib/template/index.ts` | `validateTemplate(source)`, the one entry point everything else calls |
| `src/lib/template/jsonschema.ts` | the JSON Schema generated from the Zod schema |

**Plumbing (Phase 2):**

| File | Responsibility |
| --- | --- |
| `src/lib/env.ts` | every environment variable, read lazily, never at import time |
| `src/db/schema.ts` | the Drizzle tables |
| `src/db/client.ts` | the lazy pool and the `db` handle |
| `src/db/migrate.ts` | apply migrations under a Postgres advisory lock |
| `instrumentation.ts` | run migrations once when the server starts |
| `src/lib/http.ts` | `HttpError`, `json`, and the handler wrapper that maps errors to bodies |
| `src/lib/auth/session.ts` | create, read and destroy a session |
| `src/lib/auth/github.ts` | the authorize URL, the code exchange, the account read |
| `src/lib/auth/guard.ts` | `requireUser` and `requireAdmin` |
| `src/lib/users.ts` | upsert by GitHub id, and the admin list |

**The registry (Phases 3 and 4):**

| File | Responsibility |
| --- | --- |
| `src/lib/storage.ts` | put and get an image in the bucket |
| `src/lib/templates/slug.ts` | a name to a free slug |
| `src/lib/templates/queries.ts` | reads: catalog page, one template, versions |
| `src/lib/templates/publish.ts` | writes: draft, metadata, publish a version |
| `src/lib/templates/social.ts` | likes, comments, reports |
| `src/components/templates/*` | cards, the editor, the preview, comments, the like button |
| `src/app/templates/*`, `src/app/u/[login]`, `src/app/me/templates`, `src/app/admin/reports` | the pages |
| `src/app/api/v1/**` | the API |

---

## Phase 1: the schema and the validator

No database, no UI, no authentication. Phase 1 ends with a live endpoint that validates a file and a published JSON Schema.

### Task 1: Test runner, diagnostics, and positioned YAML parsing

**Files:**
- Modify: `site/package.json` (dependencies and the `test` script)
- Create: `site/vitest.config.ts`
- Create: `site/src/lib/template/diagnostics.ts`
- Create: `site/src/lib/template/parse.ts`
- Test: `site/src/lib/template/parse.test.ts`
- Modify: `.github/workflows/ci.yml` (add `pnpm test` to the `site` job)
- Modify: `Makefile` (add `site-test`)

**Interfaces:**
- Consumes: nothing.
- Produces: `type Pos = { line: number; column: number; offset: number }`, `type Range = { start: Pos; end: Pos }`, `type Severity = "error" | "warning" | "info"`, `type Diagnostic = { severity: Severity; code: string; message: string; path: (string | number)[]; range?: Range; hint?: string }`, `blocks(diagnostics: Diagnostic[]): boolean`, `type Locate = (path: (string | number)[], target?: "value" | "key") => Range | undefined`, `parseSource(source: string): { value?: unknown; locate: Locate; diagnostics: Diagnostic[] }`.

- [ ] **Step 1: Add the dependencies and the test script**

```bash
cd site
pnpm add zod yaml semver
pnpm add -D vitest @types/semver
```

Then add to `site/package.json` scripts, keeping the existing entries:

```json
"test": "vitest run",
"test:watch": "vitest"
```

- [ ] **Step 2: Configure Vitest with the `@/` alias**

Create `site/vitest.config.ts`:

```ts
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

// The alias is Next's, repeated: vitest does not read tsconfig paths.
export default defineConfig({
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
```

- [ ] **Step 3: Write the failing test**

Create `site/src/lib/template/parse.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { parseSource } from "./parse";

describe("parseSource", () => {
  it("returns the parsed value", () => {
    const { value, diagnostics } = parseSource("project: umami\napps: []\n");

    expect(diagnostics).toEqual([]);
    expect(value).toEqual({ project: "umami", apps: [] });
  });

  it("reports a syntax error with a position", () => {
    const { value, diagnostics } = parseSource("project: umami\n  bad: indent\n");

    expect(value).toBeUndefined();
    expect(diagnostics[0].severity).toBe("error");
    expect(diagnostics[0].code).toBe("yaml.syntax");
    expect(diagnostics[0].range?.start.line).toBe(2);
  });

  it("locates a value by path", () => {
    const { locate } = parseSource("apps:\n  - name: web\n    port: 3000\n");
    const range = locate(["apps", 0, "port"]);

    expect(range?.start.line).toBe(3);
  });

  it("locates a key when asked", () => {
    const { locate } = parseSource("apps:\n  - port: 3000\n");
    const key = locate(["apps", 0, "port"], "key");
    const value = locate(["apps", 0, "port"], "value");

    expect(key?.start.column).toBe(5);
    expect(value?.start.column).toBe(11);
  });

  it("falls back to the parent when the path is absent", () => {
    const { locate } = parseSource("apps:\n  - name: web\n");

    expect(locate(["apps", 0, "port"])?.start.line).toBe(2);
  });

  it("gives an empty document one diagnostic and no value", () => {
    const { value, diagnostics } = parseSource("   \n");

    expect(value).toBeUndefined();
    expect(diagnostics[0].code).toBe("yaml.empty");
  });
});
```

- [ ] **Step 4: Run it and watch it fail**

Run: `cd site && pnpm test -- parse`
Expected: FAIL, cannot resolve `./parse`.

- [ ] **Step 5: Write the diagnostics module**

Create `site/src/lib/template/diagnostics.ts`:

```ts
export type Pos = { line: number; column: number; offset: number };
export type Range = { start: Pos; end: Pos };
export type Severity = "error" | "warning" | "info";

export type Diagnostic = {
  severity: Severity;
  // Stable and linkable: the docs have a section per code.
  code: string;
  message: string;
  path: (string | number)[];
  range?: Range;
  hint?: string;
};

// Only an error stops a publish. A warning is advice the author may keep.
export function blocks(diagnostics: Diagnostic[]): boolean {
  return diagnostics.some((d) => d.severity === "error");
}

export type Locate = (path: (string | number)[], target?: "value" | "key") => Range | undefined;
```

- [ ] **Step 6: Write the parser**

Create `site/src/lib/template/parse.ts`:

```ts
import { isMap, isSeq, LineCounter, type Node, parseDocument, type Pair } from "yaml";
import type { Diagnostic, Locate, Pos, Range } from "./diagnostics";

function position(counter: LineCounter, offset: number): Pos {
  const { line, col } = counter.linePos(offset);
  return { line, column: col, offset };
}

function rangeOf(counter: LineCounter, node: { range?: [number, number, number] }): Range | undefined {
  if (!node.range) return undefined;
  const [start, end] = node.range;
  return { start: position(counter, start), end: position(counter, end) };
}

// The key token rather than the value, for the errors that are about a
// field existing at all.
function keyRange(counter: LineCounter, parent: unknown, key: string | number): Range | undefined {
  if (!isMap(parent)) return undefined;
  const pair = parent.items.find((item: Pair) => (item.key as { value?: unknown })?.value === key);
  return pair?.key ? rangeOf(counter, pair.key as Node) : undefined;
}

export function parseSource(source: string): {
  value?: unknown;
  locate: Locate;
  diagnostics: Diagnostic[];
} {
  const counter = new LineCounter();
  const doc = parseDocument(source, { lineCounter: counter, keepSourceTokens: true });

  const locate: Locate = (path, target = "value") => {
    // Walk down as far as the document goes, so a missing key still
    // points somewhere useful: its parent.
    let deepest: Range | undefined = doc.contents ? rangeOf(counter, doc.contents as Node) : undefined;
    let parent: unknown = doc.contents;

    for (let i = 0; i < path.length; i++) {
      const node = doc.getIn(path.slice(0, i + 1), true);
      if (!node) break;
      if (i === path.length - 1 && target === "key") {
        const key = keyRange(counter, parent, path[i]);
        if (key) return key;
      }
      deepest = rangeOf(counter, node as Node) ?? deepest;
      parent = node;
    }

    return deepest;
  };

  const diagnostics: Diagnostic[] = doc.errors.map((error) => ({
    severity: "error" as const,
    code: "yaml.syntax",
    message: error.message,
    path: [],
    range: {
      start: position(counter, error.pos[0]),
      end: position(counter, error.pos[1]),
    },
  }));

  if (diagnostics.length > 0) return { locate, diagnostics };

  if (doc.contents === null || (!isMap(doc.contents) && !isSeq(doc.contents))) {
    return {
      locate,
      diagnostics: [
        {
          severity: "error",
          code: "yaml.empty",
          message: "the file is empty: a template is a mapping with at least version, project and apps",
          path: [],
        },
      ],
    };
  }

  return { value: doc.toJS(), locate, diagnostics: [] };
}
```

- [ ] **Step 7: Run the tests until they pass**

Run: `cd site && pnpm test -- parse`
Expected: PASS, six tests.

- [ ] **Step 8: Wire it into CI and the Makefile**

In `.github/workflows/ci.yml`, in the `site` job, add a step after `pnpm exec biome ci .`:

```yaml
      - run: pnpm test
```

In `Makefile`, after the `site-dev` target:

```make
.PHONY: site-test
site-test: ## Run the site's unit tests
	cd site && $(PNPM) test
```

- [ ] **Step 9: Format, lint, commit**

```bash
cd site && pnpm run format && pnpm run lint && pnpm test
cd .. && git add site/package.json site/pnpm-lock.yaml site/vitest.config.ts site/src/lib/template .github/workflows/ci.yml Makefile
git commit -m "Parse a template file into positions the author can see"
```

---

### Task 2: Shared values and the engine catalogue

**Files:**
- Create: `site/src/lib/template/values.ts`
- Create: `site/src/lib/template/engines.ts`
- Test: `site/src/lib/template/values.test.ts`
- Test: `site/src/lib/template/engines.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `slugPattern`, `reserved: { slugs: Set<string>; databases: Set<string>; stores: Set<string> }`, `parseSize(input: string | number): number | undefined`, `envKeyPattern`, `prefixPattern`, `tagPattern`, `healthPathProblem(path: string): string | undefined`, `MIN_CPU = 0.01`, `MIN_MEMORY = 6291456`, `MAX_AUTOSCALE = 100`; and `type Engine = { engine: string; versions: string[]; stem: string; port: number; hasDatabase: boolean; fixedUsername?: string; refusedUsernames?: string[] }`, `engines: Engine[]`, `findEngine(name: string): Engine | undefined`, `databaseVarNames(engine: Engine, prefix: string): string[]`, `storeVarNames(prefix: string): string[]`.

- [ ] **Step 1: Write the failing tests**

Create `site/src/lib/template/values.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { healthPathProblem, parseSize, prefixPattern, slugPattern } from "./values";

describe("parseSize", () => {
  it("reads the spellings the CLI reads", () => {
    expect(parseSize("512Mi")).toBe(536870912);
    expect(parseSize("512M")).toBe(536870912);
    expect(parseSize("2Gi")).toBe(2147483648);
    expect(parseSize("1500")).toBe(1500);
    expect(parseSize(1500)).toBe(1500);
  });

  it("refuses what is not a size", () => {
    expect(parseSize("big")).toBeUndefined();
    expect(parseSize("")).toBeUndefined();
  });
});

describe("slugPattern", () => {
  it("matches the daemon's shape", () => {
    expect(slugPattern.test("umami-db")).toBe(true);
    expect(slugPattern.test("Umami")).toBe(false);
    expect(slugPattern.test("-db")).toBe(false);
    expect(slugPattern.test("db-")).toBe(false);
  });
});

describe("prefixPattern", () => {
  it("requires upper case ending in an underscore", () => {
    expect(prefixPattern.test("ANALYTICS_")).toBe(true);
    expect(prefixPattern.test("analytics_")).toBe(false);
    expect(prefixPattern.test("ANALYTICS")).toBe(false);
  });
});

describe("healthPathProblem", () => {
  it("accepts a path", () => {
    expect(healthPathProblem("/api/heartbeat")).toBeUndefined();
  });

  it("names what is wrong", () => {
    expect(healthPathProblem("api")).toMatch(/start with/);
    expect(healthPathProblem("/a?b=1")).toMatch(/query/);
    expect(healthPathProblem(`/${"a".repeat(255)}`)).toMatch(/255/);
  });
});
```

Create `site/src/lib/template/engines.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { databaseVarNames, findEngine, storeVarNames } from "./engines";

describe("findEngine", () => {
  it("knows the five engines and their newest version first", () => {
    expect(findEngine("postgres")?.versions[0]).toBe("18");
    expect(findEngine("mariadb")?.versions).toContain("10.11");
    expect(findEngine("sqlite")).toBeUndefined();
  });
});

describe("databaseVarNames", () => {
  it("writes six names for an engine with databases", () => {
    const engine = findEngine("postgres");
    if (!engine) throw new Error("postgres is missing");

    expect(databaseVarNames(engine, "")).toEqual([
      "DATABASE_URL",
      "DATABASE_HOST",
      "DATABASE_PORT",
      "DATABASE_USER",
      "DATABASE_PASSWORD",
      "DATABASE_NAME",
    ]);
  });

  it("leaves the name out for Redis and honours the prefix", () => {
    const engine = findEngine("redis");
    if (!engine) throw new Error("redis is missing");

    expect(databaseVarNames(engine, "CACHE_")).toEqual([
      "CACHE_REDIS_URL",
      "CACHE_REDIS_HOST",
      "CACHE_REDIS_PORT",
      "CACHE_REDIS_USER",
      "CACHE_REDIS_PASSWORD",
    ]);
  });
});

describe("storeVarNames", () => {
  it("writes the six S3 names, never AWS_", () => {
    expect(storeVarNames("")).toEqual([
      "S3_ENDPOINT",
      "S3_REGION",
      "S3_BUCKET",
      "S3_ACCESS_KEY_ID",
      "S3_SECRET_ACCESS_KEY",
      "S3_PATH_STYLE",
    ]);
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd site && pnpm test -- values engines`
Expected: FAIL, neither module resolves.

- [ ] **Step 3: Write `values.ts`**

```ts
// Copied from the daemon rather than fetched: a template is validated
// here before any instance sees it. internal/slug/slug.go is the original.
export const slugPattern = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

export const reserved = {
  slugs: new Set(["settings"]),
  databases: new Set(["settings", "engines"]),
  stores: new Set(["settings", "providers"]),
};

export const envKeyPattern = /^[A-Za-z_][A-Za-z0-9_]*$/;
export const prefixPattern = /^[A-Z][A-Z0-9_]*_$/;
export const tagPattern = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/;

export const MIN_CPU = 0.01;
export const MIN_MEMORY = 6 * 1024 * 1024;
export const MAX_AUTOSCALE = 100;
export const MAX_HEALTH_PATH = 255;
export const DEFAULT_PORT = 8080;

const units: [string, number][] = [
  ["Gi", 1 << 30],
  ["G", 1 << 30],
  ["Mi", 1 << 20],
  ["M", 1 << 20],
  ["Ki", 1 << 10],
  ["K", 1 << 10],
  ["B", 1],
];

// "512M" and "512Mi" are the same request; refusing one would be
// pedantry in front of a limit. cmd/cubeship/app.go reads them the same way.
export function parseSize(input: string | number): number | undefined {
  if (typeof input === "number") return Number.isFinite(input) ? Math.trunc(input) : undefined;

  let rest = input.trim();
  let factor = 1;
  for (const [suffix, value] of units) {
    if (rest.length > suffix.length && rest.toLowerCase().endsWith(suffix.toLowerCase())) {
      factor = value;
      rest = rest.slice(0, -suffix.length);
      break;
    }
  }

  const n = Number(rest.trim());
  if (rest.trim() === "" || !Number.isFinite(n)) return undefined;
  return Math.trunc(n * factor);
}

export function healthPathProblem(path: string): string | undefined {
  if (!path.startsWith("/")) return "a health path has to start with /";
  if (path.length > MAX_HEALTH_PATH) return `a health path is at most ${MAX_HEALTH_PATH} characters`;
  if (path.includes("?")) return "a health path carries no query string";
  if (path.includes("#")) return "a health path carries no fragment";
  return undefined;
}
```

- [ ] **Step 4: Write `engines.ts`**

```ts
export type Engine = {
  engine: string;
  // Newest first: an omitted version means the newest at install time.
  versions: string[];
  stem: string;
  port: number;
  hasDatabase: boolean;
  fixedUsername?: string;
  refusedUsernames?: string[];
};

// internal/datastore/engine.go is the original. A template is checked
// here, before any instance is involved, so the table is copied.
export const engines: Engine[] = [
  { engine: "postgres", versions: ["18", "17", "16", "15"], stem: "DATABASE", port: 5432, hasDatabase: true },
  { engine: "mysql", versions: ["8.4", "8.0"], stem: "DATABASE", port: 3306, hasDatabase: true, refusedUsernames: ["root"] },
  { engine: "mariadb", versions: ["11.4", "10.11"], stem: "DATABASE", port: 3306, hasDatabase: true, refusedUsernames: ["root"] },
  { engine: "redis", versions: ["7.4", "7.2"], stem: "REDIS", port: 6379, hasDatabase: false, fixedUsername: "default" },
  { engine: "mongodb", versions: ["8.0", "7.0"], stem: "MONGO", port: 27017, hasDatabase: true },
];

export function findEngine(name: string): Engine | undefined {
  return engines.find((e) => e.engine === name);
}

export function databaseVarNames(engine: Engine, prefix: string): string[] {
  const stem = `${prefix}${engine.stem}`;
  const names = [`${stem}_URL`, `${stem}_HOST`, `${stem}_PORT`, `${stem}_USER`, `${stem}_PASSWORD`];
  if (engine.hasDatabase) names.push(`${stem}_NAME`);
  return names;
}

export function storeVarNames(prefix: string): string[] {
  const stem = `${prefix}S3`;
  return [
    `${stem}_ENDPOINT`,
    `${stem}_REGION`,
    `${stem}_BUCKET`,
    `${stem}_ACCESS_KEY_ID`,
    `${stem}_SECRET_ACCESS_KEY`,
    `${stem}_PATH_STYLE`,
  ];
}
```

- [ ] **Step 5: Run the tests until they pass**

Run: `cd site && pnpm test -- values engines`
Expected: PASS.

- [ ] **Step 6: Format and commit**

```bash
cd site && pnpm run format && pnpm test
cd .. && git add site/src/lib/template
git commit -m "Copy the daemon's slugs, sizes and engines into the validator"
```

---

### Task 3: The Zod schema, and Zod issues as diagnostics

**Files:**
- Create: `site/src/lib/template/schema.ts`
- Test: `site/src/lib/template/schema.test.ts`

**Interfaces:**
- Consumes: `Diagnostic`, `Locate` from `./diagnostics`; `slugPattern`, `tagPattern`, `envKeyPattern` from `./values`.
- Produces: `manifestSchema` (a Zod schema), `type Manifest = z.output<typeof manifestSchema>`, `fromZod(error: z.ZodError, locate: Locate): Diagnostic[]`, `keysAt(path: (string | number)[]): string[]`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { parseSource } from "./parse";
import { fromZod, manifestSchema } from "./schema";

const minimal = `version: 1
project: umami
apps:
  - key: web
    image: ghcr.io/umami-software/umami
`;

function check(source: string) {
  const { value, locate } = parseSource(source);
  const result = manifestSchema.safeParse(value);
  return { result, diagnostics: result.success ? [] : fromZod(result.error, locate) };
}

describe("manifestSchema", () => {
  it("accepts a minimal template and fills the defaults", () => {
    const { result } = check(minimal);

    expect(result.success).toBe(true);
    if (!result.success) return;
    expect(result.data.environment).toBe("production");
    expect(result.data.databases).toEqual([]);
    expect(result.data.apps[0].key).toBe("web");
  });

  it("refuses an unknown key and suggests the real one", () => {
    const { diagnostics } = check(`${minimal}    healthcheck: /up\n`);

    expect(diagnostics[0].code).toBe("schema.unknown-key");
    expect(diagnostics[0].message).toContain("healthcheck");
    expect(diagnostics[0].hint).toContain("health");
    expect(diagnostics[0].range?.start.line).toBe(6);
  });

  it("refuses a version it does not speak", () => {
    const { diagnostics } = check(minimal.replace("version: 1", "version: 2"));

    expect(diagnostics[0].path).toEqual(["version"]);
  });

  it("requires at least one app", () => {
    const { result } = check("version: 1\nproject: umami\napps: []\n");

    expect(result.success).toBe(false);
  });

  it("reads an environment variable written as a number", () => {
    const { result } = check(`${minimal}    env:\n      PORT: 3000\n`);

    expect(result.success).toBe(true);
    if (!result.success) return;
    expect(result.data.apps[0].env).toEqual({ PORT: "3000" });
  });

  it("points a missing required field at its parent", () => {
    const { diagnostics } = check("version: 1\napps:\n  - key: web\n    image: nginx\n");

    expect(diagnostics.some((d) => d.path.join(".") === "project")).toBe(true);
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd site && pnpm test -- schema`
Expected: FAIL, cannot resolve `./schema`.

- [ ] **Step 3: Write the schema**

```ts
import { z } from "zod";
import type { Diagnostic, Locate } from "./diagnostics";
import { envKeyPattern, slugPattern, tagPattern } from "./values";

const slug = z.string().regex(slugPattern, "lowercase letters, digits and dashes");
const key = z.string().regex(/^[a-zA-Z][a-zA-Z0-9_-]*$/, "a letter, then letters, digits, dash or underscore");

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
  z.strictObject({ ...inputBase, type: z.literal("secret"), generate: z.number().int().optional() }),
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
  manifest: ["version", "minCubeship", "project", "environment", "inputs", "databases", "stores", "apps"],
  input: ["key", "type", "label", "help", "required", "default", "pattern", "min", "max", "options", "generate"],
  database: ["key", "name", "engine", "version", "username", "database", "expose", "limits"],
  store: ["key", "name", "version", "buckets", "limits"],
  app: ["key", "name", "image", "tag", "repo", "ref", "build", "dockerfile", "port", "health", "domains", "attach", "env", "limits", "scale", "spread", "autoscale"],
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
  const ranked = candidates
    .map((candidate) => ({ candidate, d: distance(key.toLowerCase(), candidate.toLowerCase()) }))
    .sort((a, b) => a.d - b.d);
  const best = ranked[0];
  return best && best.d <= Math.max(2, Math.floor(key.length / 3)) ? best.candidate : undefined;
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
```

- [ ] **Step 4: Run the tests until they pass**

Run: `cd site && pnpm test -- schema`
Expected: PASS, six tests.

- [ ] **Step 5: Format and commit**

```bash
cd site && pnpm run format && pnpm test
cd .. && git add site/src/lib/template
git commit -m "Describe a template's shape once, in one schema"
```

---

### Task 4: References

**Files:**
- Create: `site/src/lib/template/references.ts`
- Test: `site/src/lib/template/references.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `type ReferenceKind = "input" | "app" | "db" | "store"`, `type Reference = { kind: ReferenceKind; key: string; attr?: string; raw: string; start: number }`, `findReferences(value: string): Reference[]`, `attributesFor: Record<ReferenceKind, string[]>`, `internalHost(project: string, environment: string, appName: string): string`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { attributesFor, findReferences, internalHost } from "./references";

describe("findReferences", () => {
  it("finds one reference and its parts", () => {
    expect(findReferences("https://${input.domain}/x")).toEqual([
      { kind: "input", key: "domain", attr: undefined, raw: "${input.domain}", start: 8 },
    ]);
  });

  it("finds several, with attributes", () => {
    const found = findReferences("${db.main.host}:${db.main.port}");

    expect(found.map((r) => r.attr)).toEqual(["host", "port"]);
    expect(found.every((r) => r.kind === "db")).toBe(true);
  });

  it("ignores what is not a reference", () => {
    expect(findReferences("$ {input.x} and ${nope}")).toEqual([]);
  });
});

describe("attributesFor", () => {
  it("gives an app three and a store two", () => {
    expect(attributesFor.app).toEqual(["internal", "host", "port"]);
    expect(attributesFor.store).toEqual(["bucket", "endpoint"]);
    expect(attributesFor.input).toEqual([]);
  });
});

describe("internalHost", () => {
  it("spells the alias the daemon attaches", () => {
    expect(internalHost("umami", "production", "web")).toBe("cubeship-umami-production-web");
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd site && pnpm test -- references`
Expected: FAIL.

- [ ] **Step 3: Write the module**

```ts
export type ReferenceKind = "input" | "app" | "db" | "store";

export type Reference = {
  kind: ReferenceKind;
  key: string;
  attr?: string;
  raw: string;
  start: number;
};

export const attributesFor: Record<ReferenceKind, string[]> = {
  input: [],
  app: ["internal", "host", "port"],
  db: ["host", "port", "user", "password", "name"],
  store: ["bucket", "endpoint"],
};

const pattern = /\$\{(input|app|db|store)\.([a-zA-Z][a-zA-Z0-9_-]*)(?:\.([a-z]+))?\}/g;

export function findReferences(value: string): Reference[] {
  return [...value.matchAll(pattern)].map((match) => ({
    kind: match[1] as ReferenceKind,
    key: match[2],
    attr: match[3],
    raw: match[0],
    start: match.index,
  }));
}

// internal/app/reference.go: the network alias every replica answers on.
export function internalHost(project: string, environment: string, appName: string): string {
  return `cubeship-${project}-${environment}-${appName}`;
}
```

- [ ] **Step 4: Run until green, then commit**

```bash
cd site && pnpm test -- references && pnpm run format
cd .. && git add site/src/lib/template
git commit -m "Read the references a template makes to itself"
```

---

### Task 5: Semantics

**Files:**
- Create: `site/src/lib/template/semantics.ts`
- Test: `site/src/lib/template/semantics.test.ts`

**Interfaces:**
- Consumes: `Manifest` from `./schema`; `Locate`, `Diagnostic` from `./diagnostics`; everything from `./values`, `./engines`, `./references`.
- Produces: `checkSemantics(manifest: Manifest, locate: Locate): Diagnostic[]`.

Every rule is a code. The codes are documented in Task 9 and must match: `key.duplicate`, `name.duplicate`, `name.reserved`, `source.missing`, `source.conflict`, `image.tagged`, `repo.scheme`, `repo.ref`, `build.missing`, `dockerfile.misplaced`, `reference.unknown`, `reference.attribute`, `domain.literal`, `attach.unknown`, `attach.kind`, `attach.bucket`, `attach.prefix`, `attach.collision`, `engine.unknown`, `engine.version`, `engine.username`, `engine.password`, `database.ignored`, `limits.cpu`, `limits.memory`, `autoscale.range`, `health.path`, `input.generate`, `version.range`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { parseSource } from "./parse";
import { manifestSchema } from "./schema";
import { checkSemantics } from "./semantics";

function codes(source: string): string[] {
  const { value, locate } = parseSource(source);
  const result = manifestSchema.safeParse(value);
  if (!result.success) throw new Error(`the fixture does not parse: ${result.error.message}`);
  return checkSemantics(result.data, locate).map((d) => d.code);
}

const ok = `version: 1
project: umami
inputs:
  - key: domain
    type: domain
    label: Where it answers
databases:
  - key: db
    engine: postgres
    version: "18"
    database: umami
apps:
  - key: web
    image: ghcr.io/umami-software/umami
    tag: postgresql-v2
    port: 3000
    health: /api/heartbeat
    domains:
      - host: \${input.domain}
    attach:
      - database: db
    limits:
      cpu: 1
      memory: 1Gi
`;

describe("checkSemantics", () => {
  it("passes a template that is right", () => {
    expect(codes(ok)).toEqual([]);
  });

  it("refuses a literal host", () => {
    expect(codes(ok.replace("${input.domain}", "analytics.example.com"))).toContain("domain.literal");
  });

  it("refuses a reference to something undeclared", () => {
    expect(codes(ok.replace("${input.domain}", "${input.nope}"))).toContain("reference.unknown");
  });

  it("refuses an attribute a kind does not have", () => {
    expect(codes(`${ok}    env:\n      URL: \${db.db.endpoint}\n`)).toContain("reference.attribute");
  });

  it("refuses a tag inside the image", () => {
    expect(codes(ok.replace("umami\n", "umami:latest\n"))).toContain("image.tagged");
  });

  it("refuses two databases at one prefix on one app", () => {
    const source = ok
      .replace("    database: umami\n", "    database: umami\n  - key: other\n    engine: postgres\n")
      .replace("      - database: db\n", "      - database: db\n      - database: other\n");
    expect(codes(source)).toContain("attach.collision");
  });

  it("refuses an engine version the daemon does not have", () => {
    expect(codes(ok.replace('version: "18"', 'version: "9"'))).toContain("engine.version");
  });

  it("refuses a user the engine refuses", () => {
    const mysql = ok.replace("engine: postgres", "engine: mysql").replace('version: "18"', 'version: "8.4"');
    expect(codes(mysql.replace("    database: umami", "    database: umami\n    username: root"))).toContain("engine.username");
  });

  it("refuses a memory that is not a size, and one below the floor", () => {
    expect(codes(ok.replace("1Gi", "loads"))).toContain("limits.memory");
    expect(codes(ok.replace("1Gi", "1Ki"))).toContain("limits.memory");
  });

  it("refuses duplicate keys and reserved names", () => {
    expect(codes(ok.replace("  - key: web", "  - key: web\n    image: nginx\n  - key: web"))).toContain("key.duplicate");
    expect(codes(ok.replace("  - key: db\n", "  - key: db\n    name: engines\n"))).toContain("name.reserved");
  });

  it("refuses an app with no source and one with two", () => {
    expect(codes(ok.replace("    image: ghcr.io/umami-software/umami\n", ""))).toContain("source.missing");
    expect(codes(ok.replace("    port: 3000", "    repo: https://github.com/x/y\n    build: railpack\n    port: 3000"))).toContain("source.conflict");
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd site && pnpm test -- semantics`
Expected: FAIL.

- [ ] **Step 3: Write the rules**

```ts
import type { Diagnostic, Locate } from "./diagnostics";
import { databaseVarNames, findEngine, storeVarNames } from "./engines";
import { attributesFor, findReferences } from "./references";
import type { Manifest } from "./schema";
import {
  healthPathProblem,
  MAX_AUTOSCALE,
  MIN_CPU,
  MIN_MEMORY,
  parseSize,
  prefixPattern,
  reserved,
  slugPattern,
} from "./values";
import { validRange } from "semver";

type Path = (string | number)[];

export function checkSemantics(manifest: Manifest, locate: Locate): Diagnostic[] {
  const found: Diagnostic[] = [];
  const add = (severity: Diagnostic["severity"], code: string, message: string, path: Path, hint?: string) => {
    found.push({ severity, code, message, path, range: locate(path, "key") ?? locate(path.slice(0, -1)), hint });
  };
  const error = (code: string, message: string, path: Path, hint?: string) => add("error", code, message, path, hint);

  const kinds = [
    { field: "inputs", items: manifest.inputs },
    { field: "databases", items: manifest.databases },
    { field: "stores", items: manifest.stores },
    { field: "apps", items: manifest.apps },
  ] as const;

  // Keys are unique within their kind; a name is unique across the whole
  // instance for a database, so within the file at the very least.
  for (const { field, items } of kinds) {
    const seenKeys = new Set<string>();
    const seenNames = new Set<string>();
    items.forEach((item, index) => {
      if (seenKeys.has(item.key)) {
        error("key.duplicate", `two ${field} share the key "${item.key}"`, [field, index, "key"]);
      }
      seenKeys.add(item.key);

      const named = item as { name?: string };
      const name = named.name ?? item.key;
      if (field !== "inputs") {
        if (!slugPattern.test(name)) {
          error("name.reserved", `"${name}" is not a valid name: lowercase letters, digits and dashes`, [field, index, "name"]);
        }
        const words =
          field === "databases" ? reserved.databases : field === "stores" ? reserved.stores : reserved.slugs;
        if (words.has(name)) {
          error("name.reserved", `"${name}" is a reserved name on an instance`, [field, index, "name"]);
        }
        if (seenNames.has(name)) {
          error("name.duplicate", `two ${field} would be called "${name}"`, [field, index, "name"]);
        }
        seenNames.add(name);
      }
    });
  }

  if (manifest.minCubeship && !validRange(manifest.minCubeship)) {
    error("version.range", `"${manifest.minCubeship}" is not a version`, ["minCubeship"]);
  }

  manifest.inputs.forEach((input, index) => {
    if (input.type === "secret" && input.generate !== undefined && input.generate < 8) {
      error("input.generate", "a generated secret is at least 8 characters", ["inputs", index, "generate"]);
    }
  });

  manifest.databases.forEach((database, index) => {
    const engine = findEngine(database.engine);
    if (!engine) {
      error("engine.unknown", `"${database.engine}" is not an engine this platform runs`, ["databases", index, "engine"], "postgres, mysql, mariadb, redis or mongodb");
      return;
    }
    if (database.version && !engine.versions.includes(database.version)) {
      error("engine.version", `${engine.engine} ${database.version} is not offered`, ["databases", index, "version"], `try ${engine.versions.join(", ")}`);
    }
    if (database.username) {
      if (engine.fixedUsername && database.username !== engine.fixedUsername) {
        error("engine.username", `${engine.engine} only has the user "${engine.fixedUsername}"`, ["databases", index, "username"]);
      }
      if (engine.refusedUsernames?.includes(database.username)) {
        error("engine.username", `${engine.engine} refuses the user "${database.username}"`, ["databases", index, "username"]);
      }
    }
    if (database.database && !engine.hasDatabase) {
      add("warning", "database.ignored", `${engine.engine} has no named databases, so this is ignored`, ["databases", index, "database"]);
    }
    checkLimits(database.limits, ["databases", index, "limits"]);
  });

  manifest.stores.forEach((store, index) => checkLimits(store.limits, ["stores", index, "limits"]));

  const inputKeys = new Set(manifest.inputs.map((i) => i.key));
  const domainInputs = new Set(manifest.inputs.filter((i) => i.type === "domain").map((i) => i.key));
  const appKeys = new Set(manifest.apps.map((a) => a.key));
  const dbKeys = new Set(manifest.databases.map((d) => d.key));
  const storeKeys = new Set(manifest.stores.map((s) => s.key));
  const known = { input: inputKeys, app: appKeys, db: dbKeys, store: storeKeys };

  const checkText = (text: string, path: Path) => {
    for (const reference of findReferences(text)) {
      if (!known[reference.kind].has(reference.key)) {
        error("reference.unknown", `${reference.raw} names no ${reference.kind} in this template`, path);
        continue;
      }
      const attributes = attributesFor[reference.kind];
      if (attributes.length === 0 && reference.attr) {
        error("reference.attribute", `${reference.raw}: an input has no attributes`, path);
      } else if (attributes.length > 0 && (!reference.attr || !attributes.includes(reference.attr))) {
        error("reference.attribute", `${reference.raw}: a ${reference.kind} has ${attributes.join(", ")}`, path);
      }
    }
  };

  manifest.apps.forEach((app, index) => {
    const at = (...rest: Path): Path => ["apps", index, ...rest];

    const building = app.repo !== undefined || app.build !== undefined;
    if (!app.image && !building) {
      error("source.missing", "an app runs an image or builds a repository", at("image"), "give image, or repo with build");
    }
    if (app.image && building) {
      error("source.conflict", "an app is either an image or a build, not both", at("image"));
    }
    if (app.image?.includes(":")) {
      error("image.tagged", "the image carries no tag: the tag is its own field", at("image"));
    }
    if (building) {
      if (!app.repo) error("source.missing", "a build needs a repo", at("repo"));
      if (!app.build) error("build.missing", "a repo needs build: dockerfile or railpack", at("build"));
      if (app.repo && !/^(https?|git):\/\//.test(app.repo)) {
        error("repo.scheme", "a repository is an http, https or git URL", at("repo"));
      }
      if (app.repo?.includes("#")) {
        error("repo.ref", "the branch goes in ref, not after a #", at("repo"));
      }
      if (app.tag) error("source.conflict", "a built app has no tag", at("tag"));
      if (app.dockerfile && app.build !== "dockerfile") {
        error("dockerfile.misplaced", "dockerfile only means something with build: dockerfile", at("dockerfile"));
      }
    }

    if (app.health) {
      const problem = healthPathProblem(app.health);
      if (problem) error("health.path", problem, at("health"));
    }

    app.domains.forEach((entry, d) => {
      const references = findReferences(entry.host);
      const single = references.length === 1 && references[0].raw === entry.host.trim();
      if (!single || references[0].kind !== "input") {
        error("domain.literal", "a domain comes from an input, never a literal host", at("domains", d, "host"), "add an input of type domain and use ${input.<key>}");
        return;
      }
      if (!domainInputs.has(references[0].key)) {
        error("domain.literal", `${references[0].raw} is not an input of type domain`, at("domains", d, "host"));
      }
    });

    const writes = new Map<string, string>();
    app.attach.forEach((entry, a) => {
      if (entry.prefix && !prefixPattern.test(entry.prefix)) {
        error("attach.prefix", "a prefix is upper case and ends in an underscore, like ANALYTICS_", at("attach", a, "prefix"));
      }
      if (Boolean(entry.database) === Boolean(entry.store)) {
        error("attach.kind", "an attachment names a database or a store, exactly one", at("attach", a));
        return;
      }
      if (entry.database) {
        const database = manifest.databases.find((d) => d.key === entry.database);
        if (!database) {
          error("attach.unknown", `no database in this template has the key "${entry.database}"`, at("attach", a, "database"));
          return;
        }
        const engine = findEngine(database.engine);
        if (!engine) return;
        for (const name of databaseVarNames(engine, entry.prefix)) {
          const owner = writes.get(name);
          if (owner) {
            error("attach.collision", `${owner} and ${entry.database} both write ${name}: give one a prefix`, at("attach", a, "prefix"));
            break;
          }
          writes.set(name, entry.database);
        }
        return;
      }
      const store = manifest.stores.find((s) => s.key === entry.store);
      if (!store) {
        const input = manifest.inputs.find((i) => i.key === entry.store && i.type === "store");
        if (!input) {
          error("attach.unknown", `no store or store input has the key "${entry.store}"`, at("attach", a, "store"));
          return;
        }
      }
      if (!entry.bucket) {
        error("attach.bucket", "a store attachment names the bucket it gives the app", at("attach", a, "bucket"));
      }
      for (const name of storeVarNames(entry.prefix)) {
        const owner = writes.get(name);
        if (owner) {
          error("attach.collision", `${owner} and ${entry.store} both write ${name}: give one a prefix`, at("attach", a, "prefix"));
          break;
        }
        writes.set(name, entry.store ?? "");
      }
    });

    for (const [name, value] of Object.entries(app.env)) {
      checkText(value, at("env", name));
      const owner = writes.get(name);
      if (owner) {
        add("warning", "attach.collision", `this overrides ${name}, which attaching ${owner} already writes`, at("env", name));
      }
    }

    checkLimits(app.limits, at("limits"));

    if (app.autoscale) {
      const { min = 1, max, cpu } = app.autoscale;
      if (max < 1 || max > MAX_AUTOSCALE) {
        error("autoscale.range", `max is between 1 and ${MAX_AUTOSCALE}`, at("autoscale", "max"));
      }
      if (min < 1 || min > max) error("autoscale.range", "min is between 1 and max", at("autoscale", "min"));
      if (cpu <= 0) error("autoscale.range", "cpu is a target above 0, where 100 is one core", at("autoscale", "cpu"));
    }
  });

  return found;

  function checkLimits(value: { cpu?: number; memory?: string | number } | undefined, path: Path) {
    if (!value) return;
    if (value.cpu !== undefined && value.cpu < MIN_CPU) {
      error("limits.cpu", `a CPU limit is at least ${MIN_CPU} of a core`, [...path, "cpu"]);
    }
    if (value.memory !== undefined) {
      const bytes = parseSize(value.memory);
      if (bytes === undefined) {
        error("limits.memory", "a memory limit is a size: 512Mi, 2Gi, 1500M", [...path, "memory"]);
      } else if (bytes < MIN_MEMORY) {
        error("limits.memory", "a memory limit is at least 6Mi", [...path, "memory"]);
      }
    }
  }
}
```

- [ ] **Step 4: Run the tests until they pass**

Run: `cd site && pnpm test -- semantics`
Expected: PASS, eleven tests. Fix the rules, not the tests, if a fixture disagrees.

- [ ] **Step 5: Format and commit**

```bash
cd site && pnpm run format && pnpm test
cd .. && git add site/src/lib/template
git commit -m "Refuse the templates a schema cannot refuse"
```

---

### Task 6: Advisories

**Files:**
- Create: `site/src/lib/template/advice.ts`
- Test: `site/src/lib/template/advice.test.ts`

**Interfaces:**
- Consumes: `Manifest`, `Locate`, `findReferences`.
- Produces: `advise(manifest: Manifest, locate: Locate): Diagnostic[]`, all of severity `warning` or `info`, codes `advice.no-health`, `advice.floating-tag`, `advice.unpinned-engine`, `advice.no-limits`, `advice.unreachable-app`, `advice.dockerhub`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { advise } from "./advice";
import { parseSource } from "./parse";
import { manifestSchema } from "./schema";

function codes(source: string): string[] {
  const { value, locate } = parseSource(source);
  const result = manifestSchema.safeParse(value);
  if (!result.success) throw new Error("the fixture does not parse");
  return advise(result.data, locate).map((d) => d.code);
}

const bare = `version: 1
project: demo
apps:
  - key: web
    image: nginx
`;

describe("advise", () => {
  it("warns about everything a bare app leaves out", () => {
    const found = codes(bare);

    expect(found).toContain("advice.no-health");
    expect(found).toContain("advice.floating-tag");
    expect(found).toContain("advice.no-limits");
    expect(found).toContain("advice.unreachable-app");
    expect(found).toContain("advice.dockerhub");
  });

  it("warns about an engine left unpinned", () => {
    expect(codes(`${bare}databases:\n  - key: db\n    engine: postgres\n`)).toContain("advice.unpinned-engine");
  });

  it("never returns an error", () => {
    const { value, locate } = parseSource(bare);
    const result = manifestSchema.safeParse(value);
    if (!result.success) throw new Error("the fixture does not parse");

    expect(advise(result.data, locate).every((d) => d.severity !== "error")).toBe(true);
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd site && pnpm test -- advice`
Expected: FAIL.

- [ ] **Step 3: Write the advisories**

```ts
import type { Diagnostic, Locate } from "./diagnostics";
import { findReferences } from "./references";
import type { Manifest } from "./schema";

type Path = (string | number)[];

export function advise(manifest: Manifest, locate: Locate): Diagnostic[] {
  const found: Diagnostic[] = [];
  const say = (severity: "warning" | "info", code: string, message: string, path: Path) => {
    found.push({ severity, code, message, path, range: locate(path, "key") ?? locate(path.slice(0, -1)) });
  };

  for (const database of manifest.databases) {
    const index = manifest.databases.indexOf(database);
    if (!database.version) {
      say("warning", "advice.unpinned-engine", "without a version, two installs of this template run different engines", ["databases", index, "engine"]);
    }
  }

  // An app nothing addresses is usually a mistake: it has no domain and
  // no other app names it.
  const referenced = new Set(
    manifest.apps.flatMap((app) =>
      Object.values(app.env).flatMap((value) => findReferences(value).filter((r) => r.kind === "app").map((r) => r.key)),
    ),
  );

  manifest.apps.forEach((app, index) => {
    const at = (...rest: Path): Path => ["apps", index, ...rest];

    if (!app.health) {
      say("warning", "advice.no-health", "with no health path, a deploy cannot tell whether this started", at("key"));
    }
    if (app.image && !app.tag) {
      say("warning", "advice.floating-tag", "without a tag this follows the registry: two installs may differ", at("image"));
    }
    if (app.image && !app.image.includes("/")) {
      say("info", "advice.dockerhub", "an unqualified image comes from Docker Hub, which rate-limits anonymous pulls", at("image"));
    }
    if (!app.limits) {
      say("warning", "advice.no-limits", "with no limits, one copy of this app can take the whole machine", at("key"));
    }
    if (app.domains.length === 0 && !referenced.has(app.key)) {
      say("warning", "advice.unreachable-app", "nothing reaches this app: it has no domain and no other app names it", at("key"));
    }
  });

  return found;
}
```

- [ ] **Step 4: Run until green, then commit**

```bash
cd site && pnpm test -- advice && pnpm run format
cd .. && git add site/src/lib/template
git commit -m "Warn about the templates that install once and rot"
```

---

### Task 7: Normalization and the one entry point

**Files:**
- Create: `site/src/lib/template/normalize.ts`
- Create: `site/src/lib/template/index.ts`
- Test: `site/src/lib/template/index.test.ts`
- Create: `site/src/lib/template/fixtures/umami.yaml`

**Interfaces:**
- Consumes: everything from Tasks 1 to 6.
- Produces: `type NormalizedManifest` (below), `normalize(manifest: Manifest): NormalizedManifest`, `type ValidationResult = { ok: boolean; diagnostics: Diagnostic[]; manifest?: NormalizedManifest }`, `validateTemplate(source: string): ValidationResult`, `SCHEMA_VERSION = 1`.

The normalized shape is snake_case because its consumer is a Go daemon, and it carries `internal_host` per app so nothing downstream has to know how the alias is spelled.

- [ ] **Step 1: Write the fixture**

Create `site/src/lib/template/fixtures/umami.yaml` with exactly the example from the design document's "The file" section.

- [ ] **Step 2: Write the failing test**

```ts
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { validateTemplate } from "./index";

const umami = readFileSync(join(__dirname, "fixtures/umami.yaml"), "utf8");

describe("validateTemplate", () => {
  it("accepts the fixture and normalizes it", () => {
    const { ok, diagnostics, manifest } = validateTemplate(umami);

    expect(diagnostics.filter((d) => d.severity === "error")).toEqual([]);
    expect(ok).toBe(true);
    expect(manifest?.schema_version).toBe(1);
    expect(manifest?.environment).toBe("production");
    expect(manifest?.apps[0].source).toEqual({
      type: "image",
      image: "ghcr.io/umami-software/umami",
      tag: "postgresql-v2",
    });
    expect(manifest?.apps[0].internal_host).toBe("cubeship-umami-production-web");
    expect(manifest?.apps[0].limits).toEqual({ cpu: 1, memory_bytes: 1073741824 });
    expect(manifest?.apps[0].domains[0]).toEqual({ host: "${input.domain}", port: 3000 });
    expect(manifest?.databases[0].name).toBe("umami-db");
  });

  it("sorts environment variables, so two publishes of one file are one document", () => {
    const { manifest } = validateTemplate(umami);

    expect(Object.keys(manifest?.apps[0].env ?? {})).toEqual(["APP_SECRET", "DATABASE_TYPE"]);
  });

  it("stops at a syntax error without running the rest", () => {
    const { ok, diagnostics, manifest } = validateTemplate("project: x\n  bad\n");

    expect(ok).toBe(false);
    expect(manifest).toBeUndefined();
    expect(diagnostics.every((d) => d.code === "yaml.syntax")).toBe(true);
  });

  it("keeps warnings and still succeeds", () => {
    const { ok, diagnostics } = validateTemplate("version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n");

    expect(ok).toBe(true);
    expect(diagnostics.some((d) => d.severity === "warning")).toBe(true);
  });

  it("does not normalize when a rule was broken", () => {
    const broken = umami.replace("${input.domain}", "analytics.example.com");

    expect(validateTemplate(broken).manifest).toBeUndefined();
  });
});
```

- [ ] **Step 3: Run it and watch it fail**

Run: `cd site && pnpm test -- template/index`
Expected: FAIL.

- [ ] **Step 4: Write `normalize.ts`**

```ts
import { findEngine } from "./engines";
import { internalHost } from "./references";
import type { Manifest } from "./schema";
import { DEFAULT_PORT, parseSize } from "./values";

export const SCHEMA_VERSION = 1;

export type NormalizedLimits = { cpu: number | null; memory_bytes: number | null };

export type NormalizedSource =
  | { type: "image"; image: string; tag: string | null }
  | { type: "dockerfile" | "railpack"; repo: string; ref: string | null; dockerfile: string | null };

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
  stores: { key: string; name: string; version: string | null; buckets: string[]; limits: NormalizedLimits | null }[];
  apps: NormalizedApp[];
};

function limitsOf(value: { cpu?: number; memory?: string | number } | undefined): NormalizedLimits | null {
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
        domains: app.domains.map((entry) => ({ host: entry.host, port: entry.port ?? app.port ?? DEFAULT_PORT })),
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
        autoscale: app.autoscale ? { min: app.autoscale.min ?? 1, max: app.autoscale.max, cpu: app.autoscale.cpu } : null,
        internal_host: internalHost(manifest.project, manifest.environment, name),
      };
    }),
  };
}
```

- [ ] **Step 5: Write `index.ts`**

```ts
import { advise } from "./advice";
import { blocks, type Diagnostic } from "./diagnostics";
import { normalize, type NormalizedManifest } from "./normalize";
import { parseSource } from "./parse";
import { fromZod, manifestSchema } from "./schema";
import { checkSemantics } from "./semantics";

export type ValidationResult = {
  ok: boolean;
  diagnostics: Diagnostic[];
  manifest?: NormalizedManifest;
};

export { SCHEMA_VERSION } from "./normalize";
export type { NormalizedManifest } from "./normalize";
export type { Diagnostic } from "./diagnostics";

// Each layer runs only when the one before it left something to work
// with: semantics over a half-parsed file would report nonsense.
export function validateTemplate(source: string): ValidationResult {
  const { value, locate, diagnostics } = parseSource(source);
  if (!value) return { ok: false, diagnostics };

  const parsed = manifestSchema.safeParse(value);
  if (!parsed.success) return { ok: false, diagnostics: fromZod(parsed.error, locate) };

  const found = [...checkSemantics(parsed.data, locate), ...advise(parsed.data, locate)];
  if (blocks(found)) return { ok: false, diagnostics: found };

  return { ok: true, diagnostics: found, manifest: normalize(parsed.data) };
}
```

- [ ] **Step 6: Run the tests until they pass**

Run: `cd site && pnpm test`
Expected: PASS, every file.

- [ ] **Step 7: Format and commit**

```bash
cd site && pnpm run format && pnpm test
cd .. && git add site/src/lib/template
git commit -m "Turn a validated template into the document an instance reads"
```

---

### Task 8: The schema document and the validate endpoint

**Files:**
- Create: `site/src/lib/template/jsonschema.ts`
- Create: `site/src/lib/http.ts`
- Create: `site/src/app/schema/template/v1.json/route.ts`
- Create: `site/src/app/api/v1/validate/route.ts`
- Test: `site/src/lib/template/jsonschema.test.ts`
- Test: `site/src/app/api/v1/validate/route.test.ts`

**Interfaces:**
- Consumes: `manifestSchema`, `validateTemplate`.
- Produces: `templateJsonSchema(): object`; `class HttpError extends Error { status: number; code: string; extra?: Record<string, unknown> }`, `json(data: unknown, init?: ResponseInit): Response`, `fail(error: unknown): Response`, `route(handler: (request: Request, context: { params: Promise<Record<string, string>> }) => Promise<Response>)`.

`src/lib/http.ts` is listed under Phase 2 plumbing in the file structure; it is first needed here, so it is built here.

- [ ] **Step 1: Write the failing tests**

```ts
// jsonschema.test.ts
import { describe, expect, it } from "vitest";
import { templateJsonSchema } from "./jsonschema";

describe("templateJsonSchema", () => {
  it("describes the manifest and refuses extra keys", () => {
    const schema = templateJsonSchema() as Record<string, any>;

    expect(schema.$schema).toContain("json-schema.org");
    expect(schema.properties.apps).toBeDefined();
    expect(schema.properties.project).toBeDefined();
    expect(schema.additionalProperties).toBe(false);
  });
});
```

```ts
// route.test.ts
import { describe, expect, it } from "vitest";
import { POST } from "./route";

async function post(body: string, type = "text/yaml") {
  const response = await POST(new Request("http://localhost/api/v1/validate", {
    method: "POST",
    headers: { "content-type": type },
    body,
  }));
  return { status: response.status, body: await response.json() };
}

describe("POST /api/v1/validate", () => {
  it("returns the manifest for a valid file", async () => {
    const { status, body } = await post("version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n    tag: '1'\n");

    expect(status).toBe(200);
    expect(body.ok).toBe(true);
    expect(body.manifest.apps[0].name).toBe("web");
  });

  it("returns diagnostics with a 422 for a broken one", async () => {
    const { status, body } = await post("version: 1\napps: []\n");

    expect(status).toBe(422);
    expect(body.ok).toBe(false);
    expect(body.diagnostics.length).toBeGreaterThan(0);
  });

  it("refuses a body that is too large", async () => {
    const { status, body } = await post("x".repeat(256 * 1024 + 1));

    expect(status).toBe(413);
    expect(body.error.code).toBe("too_large");
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd site && pnpm test -- jsonschema validate`
Expected: FAIL.

- [ ] **Step 3: Write `http.ts`**

```ts
export class HttpError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly extra?: Record<string, unknown>,
  ) {
    super(message);
  }
}

export function json(data: unknown, init?: ResponseInit): Response {
  return Response.json(data, init);
}

export function fail(error: unknown): Response {
  if (error instanceof HttpError) {
    return json({ error: { code: error.code, message: error.message }, ...error.extra }, { status: error.status });
  }
  // Anything unplanned is ours, and its message is not the caller's business.
  console.error(error);
  return json({ error: { code: "internal", message: "something went wrong here" } }, { status: 500 });
}

// context is optional so a test can call a handler with a Request alone.
type Handler = (
  request: Request,
  context?: { params: Promise<Record<string, string>> },
) => Promise<Response>;

export function route(handler: Handler): Handler {
  return async (request, context) => {
    try {
      return await handler(request, context);
    } catch (error) {
      return fail(error);
    }
  };
}

export async function readText(request: Request, limit = 256 * 1024): Promise<string> {
  const text = await request.text();
  if (text.length > limit) {
    throw new HttpError(413, "too_large", `a template file is at most ${Math.floor(limit / 1024)} KB`);
  }
  return text;
}
```

- [ ] **Step 4: Write `jsonschema.ts`**

```ts
import { z } from "zod";
import { manifestSchema } from "./schema";

// io: "input" describes what an author writes rather than what the parser
// produces, which is the difference between a document that validates
// their file and one that validates ours.
export function templateJsonSchema(): object {
  return {
    ...z.toJSONSchema(manifestSchema, { io: "input", unrepresentable: "any" }),
    $id: "https://cubeship.dev/schema/template/v1.json",
    title: "Cubeship template, version 1",
  };
}
```

- [ ] **Step 5: Write the two routes**

`src/app/schema/template/v1.json/route.ts`:

```ts
import { templateJsonSchema } from "@/lib/template/jsonschema";

// The path is permanent: editors and agents point at it forever.
export const dynamic = "force-static";

export function GET() {
  return Response.json(templateJsonSchema(), {
    headers: { "cache-control": "public, max-age=3600", "access-control-allow-origin": "*" },
  });
}
```

`src/app/api/v1/validate/route.ts`:

```ts
import { readText, route } from "@/lib/http";
import { validateTemplate } from "@/lib/template";

export const POST = route(async (request) => {
  const source = await readText(request);
  const result = validateTemplate(source);

  return Response.json(result, {
    status: result.ok ? 200 : 422,
    headers: { "access-control-allow-origin": "*" },
  });
});
```

- [ ] **Step 6: Run the tests until they pass**

Run: `cd site && pnpm test`
Expected: PASS. If `z.toJSONSchema` refuses the transform on environment values, give that union `.meta({ type: "string" })` rather than removing the transform.

- [ ] **Step 7: Check it in a browser**

```bash
cd site && pnpm dev
```

Open `http://localhost:3002/schema/template/v1.json` and confirm it renders. Then:

```bash
curl -sS -X POST --data-binary @src/lib/template/fixtures/umami.yaml http://localhost:3002/api/v1/validate | head -40
```

- [ ] **Step 8: Format, typecheck, commit**

```bash
cd site && pnpm run format && pnpm run lint && pnpm run typecheck && pnpm test
cd .. && git add site/src/lib site/src/app/schema site/src/app/api
git commit -m "Publish the template schema and validate a file over HTTP"
```

---

### Task 9: Document the format

**Files:**
- Create: `site/content/docs/templates/index.mdx`
- Create: `site/content/docs/templates/file.mdx`
- Create: `site/content/docs/templates/inputs.mdx`
- Create: `site/content/docs/templates/references.mdx`
- Create: `site/content/docs/templates/diagnostics.mdx`
- Create: `site/content/docs/templates/meta.json`
- Modify: `site/content/docs/meta.json`

**Interfaces:**
- Consumes: the codes from Tasks 5 and 6. Every code must appear in `diagnostics.mdx`.
- Produces: nothing code depends on.

- [ ] **Step 1: Write the pages**

`index.mdx` says what a template is, who writes one, and what it cannot carry — the four refusals from the design document, each with its reason. `file.mdx` walks the fixture block by block, every field, with the defaults. `inputs.mdx` covers the six input types and which fields each one takes. `references.mdx` covers the grammar, the attributes per kind, and that the installer resolves them, with the internal-host example spelled out. `diagnostics.mdx` is a table of every code, its severity, and what to do about it.

Each page carries frontmatter in the shape the other pages use:

```mdx
---
title: The template file
description: Every block a template may contain, and the defaults for the ones it leaves out
---
```

`templates/meta.json`:

```json
{
  "title": "Templates",
  "pages": ["index", "file", "inputs", "references", "diagnostics"]
}
```

- [ ] **Step 2: Add the section to the docs order**

In `site/content/docs/meta.json`, add `"templates"` to `pages`, after `"mcp"`.

- [ ] **Step 3: Check every code is documented**

```bash
cd site
grep -ohE '"(schema|yaml|reference|domain|attach|engine|limits|autoscale|health|input|version|key|name|source|image|repo|build|dockerfile|database|advice)\.[a-z-]+"' src/lib/template/*.ts | tr -d '"' | sort -u > /tmp/codes.txt
grep -oE '`(schema|yaml|reference|domain|attach|engine|limits|autoscale|health|input|version|key|name|source|image|repo|build|dockerfile|database|advice)\.[a-z-]+`' content/docs/templates/diagnostics.mdx | tr -d '`' | sort -u > /tmp/documented.txt
diff /tmp/codes.txt /tmp/documented.txt
```

Expected: no output. Add the missing rows until there is none.

- [ ] **Step 4: Build and commit**

```bash
cd site && pnpm run build
cd .. && git add site/content/docs
git commit -m "Document the template file, field by field and code by code"
```

---

## Phase 2: Postgres and GitHub sign-in

Phase 2 ends with a signed-in header on localhost and a database that migrates itself.

### Task 10: Environment, tables, migrations

**Files:**
- Create: `site/src/lib/env.ts`, `site/src/db/schema.ts`, `site/src/db/client.ts`, `site/src/db/migrate.ts`, `site/src/db/testing.ts`, `site/drizzle.config.ts`, `site/instrumentation.ts`
- Create: `site/drizzle/0000_*.sql` (generated, committed)
- Modify: `site/next.config.ts`, `site/package.json`, `Makefile`, `.github/workflows/ci.yml`, `Dockerfile.site`, `docs/design/templates.md`
- Test: `site/src/lib/env.test.ts`, `site/src/db/schema.db.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `databaseUrl()`, `githubOAuth()`, `adminLogins()`, `s3Config()`, `publicUrl()` from `@/lib/env`; `db()` from `@/db/client`; the tables `users`, `sessions`, `templates`, `templateVersions`, `likes`, `comments`, `reports` from `@/db/schema`; `runMigrations()` from `@/db/migrate`; `withDatabase()` from `@/db/testing`.

- [ ] **Step 1: Add the dependencies**

```bash
cd site
pnpm add drizzle-orm pg
pnpm add -D drizzle-kit @types/pg
```

Add to `site/package.json` scripts: `"db:generate": "drizzle-kit generate"`.

- [ ] **Step 2: Write the failing env test**

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { adminLogins, databaseUrl, publicUrl } from "./env";

afterEach(() => vi.unstubAllEnvs());

describe("env", () => {
  it("throws only when asked, never at import", () => {
    vi.stubEnv("DATABASE_URL", "");
    expect(() => databaseUrl()).toThrow(/DATABASE_URL/);
  });

  it("reads the admin list as lower-case logins", () => {
    vi.stubEnv("ADMIN_LOGINS", " Lucas , someoneElse ");
    expect(adminLogins()).toEqual(["lucas", "someoneelse"]);
  });

  it("falls back to the development origin", () => {
    vi.stubEnv("SITE_URL", "");
    expect(publicUrl()).toBe("http://localhost:3002");
  });
});
```

Run: `cd site && pnpm test -- env` — expected FAIL.

- [ ] **Step 3: Write `src/lib/env.ts`**

```ts
// Every variable is read through a function. Reading one at import time
// would make `next build`, which has none of them, fail.
function need(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is not set`);
  return value;
}

export function databaseUrl(): string {
  return need("DATABASE_URL");
}

export function githubOAuth(): { clientId: string; clientSecret: string } {
  return { clientId: need("GITHUB_CLIENT_ID"), clientSecret: need("GITHUB_CLIENT_SECRET") };
}

export function adminLogins(): string[] {
  return (process.env.ADMIN_LOGINS ?? "")
    .split(",")
    .map((login) => login.trim().toLowerCase())
    .filter(Boolean);
}

export function s3Config(): {
  endpoint: string;
  region: string;
  bucket: string;
  accessKeyId: string;
  secretAccessKey: string;
  pathStyle: boolean;
} {
  return {
    endpoint: need("S3_ENDPOINT"),
    region: process.env.S3_REGION || "us-east-1",
    bucket: need("S3_BUCKET"),
    accessKeyId: need("S3_ACCESS_KEY_ID"),
    secretAccessKey: need("S3_SECRET_ACCESS_KEY"),
    // MinIO needs path style; a bucket-per-host provider does not.
    pathStyle: (process.env.S3_PATH_STYLE ?? "true") !== "false",
  };
}

export function publicUrl(): string {
  return process.env.SITE_URL || "http://localhost:3002";
}
```

- [ ] **Step 4: Write the tables**

`src/db/schema.ts`:

```ts
import { relations } from "drizzle-orm";
import {
  type AnyPgColumn,
  bigint,
  index,
  integer,
  jsonb,
  pgTable,
  primaryKey,
  serial,
  text,
  timestamp,
  unique,
} from "drizzle-orm/pg-core";

export const users = pgTable("users", {
  id: serial("id").primaryKey(),
  githubId: bigint("github_id", { mode: "number" }).notNull().unique(),
  login: text("login").notNull(),
  name: text("name"),
  avatarUrl: text("avatar_url"),
  role: text("role").notNull().default("user"),
  blockedAt: timestamp("blocked_at", { withTimezone: true }),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const sessions = pgTable("sessions", {
  // The cookie carries the token; only its hash is stored, so a dump of
  // this table is not a drawer of live sessions.
  tokenHash: text("token_hash").primaryKey(),
  userId: integer("user_id")
    .notNull()
    .references(() => users.id, { onDelete: "cascade" }),
  expiresAt: timestamp("expires_at", { withTimezone: true }).notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const templates = pgTable(
  "templates",
  {
    id: serial("id").primaryKey(),
    slug: text("slug").notNull().unique(),
    authorId: integer("author_id")
      .notNull()
      .references(() => users.id),
    name: text("name").notNull(),
    summary: text("summary").notNull(),
    imageKey: text("image_key"),
    tags: text("tags").array().notNull().default([]),
    status: text("status").notNull().default("draft"),
    likesCount: integer("likes_count").notNull().default(0),
    // No foreign key: templates and template_versions reference each
    // other, and the publish transaction is what keeps this honest.
    currentVersionId: integer("current_version_id"),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (table) => [index("templates_status_idx").on(table.status), index("templates_author_idx").on(table.authorId)],
);

export const templateVersions = pgTable(
  "template_versions",
  {
    id: serial("id").primaryKey(),
    templateId: integer("template_id")
      .notNull()
      .references(() => templates.id, { onDelete: "cascade" }),
    number: integer("number").notNull(),
    manifest: jsonb("manifest").notNull(),
    source: text("source").notNull(),
    schemaVersion: integer("schema_version").notNull(),
    notes: text("notes"),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (table) => [unique("template_versions_number").on(table.templateId, table.number)],
);

export const likes = pgTable(
  "likes",
  {
    templateId: integer("template_id")
      .notNull()
      .references(() => templates.id, { onDelete: "cascade" }),
    userId: integer("user_id")
      .notNull()
      .references(() => users.id, { onDelete: "cascade" }),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (table) => [primaryKey({ columns: [table.templateId, table.userId] })],
);

export const comments = pgTable(
  "comments",
  {
    id: serial("id").primaryKey(),
    templateId: integer("template_id")
      .notNull()
      .references(() => templates.id, { onDelete: "cascade" }),
    authorId: integer("author_id")
      .notNull()
      .references(() => users.id),
    parentId: integer("parent_id").references((): AnyPgColumn => comments.id, { onDelete: "cascade" }),
    body: text("body").notNull(),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
    // Soft, so a removed comment keeps the thread's shape.
    deletedAt: timestamp("deleted_at", { withTimezone: true }),
  },
  (table) => [index("comments_template_idx").on(table.templateId)],
);

export const reports = pgTable("reports", {
  id: serial("id").primaryKey(),
  subjectType: text("subject_type").notNull(),
  subjectId: integer("subject_id").notNull(),
  reporterId: integer("reporter_id")
    .notNull()
    .references(() => users.id),
  reason: text("reason").notNull(),
  note: text("note"),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  resolvedAt: timestamp("resolved_at", { withTimezone: true }),
  resolution: text("resolution"),
});

export const templateRelations = relations(templates, ({ one, many }) => ({
  author: one(users, { fields: [templates.authorId], references: [users.id] }),
  versions: many(templateVersions),
}));
```

- [ ] **Step 5: Write the client, the migrator and the instrumentation hook**

`src/db/client.ts`:

```ts
import { drizzle } from "drizzle-orm/node-postgres";
import { Pool } from "pg";
import { databaseUrl } from "@/lib/env";
import * as schema from "./schema";

let handle: ReturnType<typeof drizzle<typeof schema>> | undefined;

// Lazy, so importing this module never opens a connection and never
// reads an environment variable.
export function db() {
  if (!handle) {
    handle = drizzle(new Pool({ connectionString: databaseUrl(), max: 10 }), { schema });
  }
  return handle;
}
```

`src/db/migrate.ts`:

```ts
import { sql } from "drizzle-orm";
import { migrate } from "drizzle-orm/node-postgres/migrator";
import { db } from "./client";

// One arbitrary constant, held for the whole migration: two containers
// starting at once must not both apply the same file.
const LOCK = 4_212_001;

export async function runMigrations(): Promise<void> {
  const handle = db();
  await handle.execute(sql`select pg_advisory_lock(${LOCK})`);
  try {
    await migrate(handle, { migrationsFolder: "./drizzle" });
  } finally {
    await handle.execute(sql`select pg_advisory_unlock(${LOCK})`);
  }
}
```

`site/instrumentation.ts`:

```ts
// Next calls register() once when the server starts, which is the only
// hook that runs in the standalone image without shipping a second
// entrypoint script and its own node_modules.
export async function register() {
  if (process.env.NEXT_RUNTIME !== "nodejs") return;
  if (process.env.NEXT_PHASE === "phase-production-build") return;
  if (!process.env.DATABASE_URL) {
    console.warn("DATABASE_URL is not set: skipping migrations");
    return;
  }

  const { runMigrations } = await import("@/db/migrate");
  await runMigrations();
}
```

`site/drizzle.config.ts`:

```ts
import { defineConfig } from "drizzle-kit";

// Read at module scope on purpose: this file is only ever loaded by the
// drizzle-kit CLI, never by the server.
export default defineConfig({
  schema: "./src/db/schema.ts",
  out: "./drizzle",
  dialect: "postgresql",
  dbCredentials: { url: process.env.DATABASE_URL ?? "" },
});
```

- [ ] **Step 6: Teach the build to carry the migrations**

In `site/next.config.ts`, add to the config object:

```ts
  // standalone traces imports; .sql files are read at run time, so they
  // have to be named explicitly or the container starts without them.
  outputFileTracingIncludes: { "/**": ["./drizzle/**"] },
  serverExternalPackages: ["pg", "sharp"],
```

- [ ] **Step 7: Start a database and generate the first migration**

Add to `Makefile`:

```make
.PHONY: site-db-up
site-db-up: ## Start the site's Postgres for development, on 5434
	docker run -d --rm --name cubeship-site-db -p 5434:5432 \
		-e POSTGRES_PASSWORD=site -e POSTGRES_USER=site -e POSTGRES_DB=site postgres:18-alpine

.PHONY: site-db-down
site-db-down: ## Stop it
	docker stop cubeship-site-db
```

Then:

```bash
make site-db-up
cd site
export DATABASE_URL=postgres://site:site@localhost:5434/site
pnpm run db:generate
```

Expected: a new file under `site/drizzle/`. Commit it; never edit it afterwards.

- [ ] **Step 8: Write the database test helper and a smoke test**

`src/db/testing.ts`:

```ts
import { sql } from "drizzle-orm";
import { db } from "./client";
import { runMigrations } from "./migrate";

// Tests that need Postgres are skipped when there is none, the way the
// Go suite skips its DB tests with -short. CI always sets this.
export const testDatabaseUrl = process.env.SITE_TEST_DATABASE_URL;

export async function withDatabase(): Promise<ReturnType<typeof db>> {
  process.env.DATABASE_URL = testDatabaseUrl;
  await runMigrations();
  const handle = db();
  await handle.execute(
    sql`truncate reports, comments, likes, template_versions, templates, sessions, users restart identity cascade`,
  );
  return handle;
}
```

`src/db/schema.db.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { users } from "./schema";
import { testDatabaseUrl, withDatabase } from "./testing";

describe.skipIf(!testDatabaseUrl)("the schema", () => {
  it("migrates and holds a user", async () => {
    const handle = await withDatabase();
    const [user] = await handle
      .insert(users)
      .values({ githubId: 1, login: "lucas" })
      .returning();

    expect(user.role).toBe("user");
  });

  it("refuses a second user with the same GitHub id", async () => {
    const handle = await withDatabase();
    await handle.insert(users).values({ githubId: 1, login: "lucas" });

    await expect(handle.insert(users).values({ githubId: 1, login: "other" })).rejects.toThrow();
  });
});
```

Run: `cd site && SITE_TEST_DATABASE_URL=postgres://site:site@localhost:5434/site pnpm test -- schema.db`
Expected: PASS.

- [ ] **Step 9: Give CI a Postgres**

In `.github/workflows/ci.yml`, in the `site` job, add above `steps:`:

```yaml
    services:
      postgres:
        image: postgres:18-alpine
        env:
          POSTGRES_USER: site
          POSTGRES_PASSWORD: site
          POSTGRES_DB: site
        ports: ["5434:5432"]
        options: >-
          --health-cmd pg_isready --health-interval 10s --health-timeout 5s --health-retries 5
```

and change the test step to:

```yaml
      - run: pnpm test
        env:
          SITE_TEST_DATABASE_URL: postgres://site:site@localhost:5434/site
```

- [ ] **Step 10: Correct the design document and the Dockerfile comment**

In `docs/design/templates.md`, replace the "Migrations run in the entrypoint" bullet with the instrumentation hook and its reason, and remove `SESSION_SECRET` from the variable table, adding one sentence: session tokens are random and stored hashed, and the OAuth state is compared rather than signed, so there is no secret to keep.

In `Dockerfile.site`, the header comment says "It talks to nothing". Replace that sentence: it now reads Postgres and an S3-compatible bucket, both given to it by the instance it runs on.

- [ ] **Step 11: Format, build, commit**

```bash
cd site && pnpm run format && pnpm run lint && pnpm run typecheck && pnpm run build
cd .. && git add site/src/db site/src/lib/env.ts site/drizzle site/drizzle.config.ts site/instrumentation.ts site/next.config.ts site/package.json site/pnpm-lock.yaml Makefile .github/workflows/ci.yml Dockerfile.site docs/design/templates.md
git commit -m "Give the site a database it migrates on its own"
```

---

### Task 11: Sessions, users, guards

**Files:**
- Create: `site/src/lib/auth/session.ts`, `site/src/lib/auth/guard.ts`, `site/src/lib/users.ts`
- Test: `site/src/lib/auth/session.db.test.ts`, `site/src/lib/users.db.test.ts`

**Interfaces:**
- Consumes: `db()`, `users`, `sessions`, `adminLogins()`, `HttpError`.
- Produces: `SESSION_COOKIE = "cubeship_session"`, `createSession(userId: number): Promise<{ token: string; expiresAt: Date }>`, `currentUser(): Promise<SessionUser | null>`, `destroySession(): Promise<void>`, `type SessionUser = { id: number; login: string; name: string | null; avatarUrl: string | null; role: "user" | "admin" }`, `requireUser(): Promise<SessionUser>`, `requireAdmin(): Promise<SessionUser>`, `upsertGithubUser(account: { id: number; login: string; name: string | null; avatarUrl: string | null }): Promise<SessionUser>`.

- [ ] **Step 1: Write the failing tests**

```ts
// session.db.test.ts
import { describe, expect, it, vi } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";

const store = new Map<string, string>();
vi.mock("next/headers", () => ({
  cookies: async () => ({
    get: (name: string) => (store.has(name) ? { name, value: store.get(name) } : undefined),
    set: (name: string, value: string) => void store.set(name, value),
    delete: (name: string) => void store.delete(name),
  }),
}));

describe.skipIf(!testDatabaseUrl)("sessions", () => {
  it("round-trips a signed-in user and stores only a hash", async () => {
    const handle = await withDatabase();
    store.clear();
    const { createSession, currentUser, SESSION_COOKIE } = await import("./session");
    const { sessions } = await import("@/db/schema");

    const [user] = await handle.insert(users).values({ githubId: 7, login: "lucas" }).returning();
    const { token } = await createSession(user.id);

    expect(store.get(SESSION_COOKIE)).toBe(token);
    const rows = await handle.select().from(sessions);
    expect(rows[0].tokenHash).not.toBe(token);
    expect(await currentUser()).toMatchObject({ id: user.id, login: "lucas" });
  });

  it("ignores an expired session", async () => {
    const handle = await withDatabase();
    store.clear();
    const { createSession, currentUser } = await import("./session");
    const { sessions } = await import("@/db/schema");
    const [user] = await handle.insert(users).values({ githubId: 8, login: "x" }).returning();

    await createSession(user.id);
    await handle.update(sessions).set({ expiresAt: new Date(Date.now() - 1000) });

    expect(await currentUser()).toBeNull();
  });
});
```

```ts
// users.db.test.ts
import { describe, expect, it, vi } from "vitest";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { upsertGithubUser } from "./users";

describe.skipIf(!testDatabaseUrl)("upsertGithubUser", () => {
  it("creates once and updates afterwards", async () => {
    await withDatabase();
    const first = await upsertGithubUser({ id: 1, login: "lucas", name: "Lucas", avatarUrl: null });
    const second = await upsertGithubUser({ id: 1, login: "lucas-l", name: "Lucas L", avatarUrl: "u" });

    expect(second.id).toBe(first.id);
    expect(second.login).toBe("lucas-l");
  });

  it("makes the listed logins admin", async () => {
    await withDatabase();
    vi.stubEnv("ADMIN_LOGINS", "lucas");
    const user = await upsertGithubUser({ id: 2, login: "Lucas", name: null, avatarUrl: null });

    expect(user.role).toBe("admin");
    vi.unstubAllEnvs();
  });
});
```

Run both — expected FAIL.

- [ ] **Step 2: Write `session.ts`**

```ts
import { createHash, randomBytes } from "node:crypto";
import { and, eq, gt } from "drizzle-orm";
import { cookies } from "next/headers";
import { db } from "@/db/client";
import { sessions, users } from "@/db/schema";

export const SESSION_COOKIE = "cubeship_session";
const THIRTY_DAYS = 30 * 24 * 60 * 60;

export type SessionUser = {
  id: number;
  login: string;
  name: string | null;
  avatarUrl: string | null;
  role: "user" | "admin";
};

function hash(token: string): string {
  return createHash("sha256").update(token).digest("hex");
}

export async function createSession(userId: number): Promise<{ token: string; expiresAt: Date }> {
  const token = randomBytes(32).toString("base64url");
  const expiresAt = new Date(Date.now() + THIRTY_DAYS * 1000);

  await db().insert(sessions).values({ tokenHash: hash(token), userId, expiresAt });
  const jar = await cookies();
  jar.set(SESSION_COOKIE, token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: THIRTY_DAYS,
  });

  return { token, expiresAt };
}

export async function currentUser(): Promise<SessionUser | null> {
  const token = (await cookies()).get(SESSION_COOKIE)?.value;
  if (!token) return null;

  const [row] = await db()
    .select({
      id: users.id,
      login: users.login,
      name: users.name,
      avatarUrl: users.avatarUrl,
      role: users.role,
      blockedAt: users.blockedAt,
    })
    .from(sessions)
    .innerJoin(users, eq(users.id, sessions.userId))
    .where(and(eq(sessions.tokenHash, hash(token)), gt(sessions.expiresAt, new Date())))
    .limit(1);

  if (!row || row.blockedAt) return null;
  return { id: row.id, login: row.login, name: row.name, avatarUrl: row.avatarUrl, role: row.role as "user" | "admin" };
}

export async function destroySession(): Promise<void> {
  const jar = await cookies();
  const token = jar.get(SESSION_COOKIE)?.value;
  if (token) await db().delete(sessions).where(eq(sessions.tokenHash, hash(token)));
  jar.delete(SESSION_COOKIE);
}
```

- [ ] **Step 3: Write `users.ts` and `guard.ts`**

```ts
// users.ts
import { eq } from "drizzle-orm";
import { db } from "@/db/client";
import { users } from "@/db/schema";
import { adminLogins } from "@/lib/env";
import type { SessionUser } from "./auth/session";

export async function upsertGithubUser(account: {
  id: number;
  login: string;
  name: string | null;
  avatarUrl: string | null;
}): Promise<SessionUser> {
  const role = adminLogins().includes(account.login.toLowerCase()) ? "admin" : "user";
  const [row] = await db()
    .insert(users)
    .values({ githubId: account.id, login: account.login, name: account.name, avatarUrl: account.avatarUrl, role })
    .onConflictDoUpdate({
      target: users.githubId,
      set: { login: account.login, name: account.name, avatarUrl: account.avatarUrl, role },
    })
    .returning();

  return { id: row.id, login: row.login, name: row.name, avatarUrl: row.avatarUrl, role: row.role as "user" | "admin" };
}

export async function findByLogin(login: string) {
  const [row] = await db().select().from(users).where(eq(users.login, login)).limit(1);
  return row;
}
```

```ts
// guard.ts
import { HttpError } from "@/lib/http";
import { currentUser, type SessionUser } from "./session";

export async function requireUser(): Promise<SessionUser> {
  const user = await currentUser();
  if (!user) throw new HttpError(401, "unauthenticated", "sign in with GitHub first");
  return user;
}

export async function requireAdmin(): Promise<SessionUser> {
  const user = await requireUser();
  if (user.role !== "admin") throw new HttpError(403, "forbidden", "this is for administrators");
  return user;
}
```

- [ ] **Step 4: Run the tests until they pass**

Run: `cd site && SITE_TEST_DATABASE_URL=postgres://site:site@localhost:5434/site pnpm test`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
cd site && pnpm run format && pnpm run typecheck
cd .. && git add site/src/lib
git commit -m "Hold a session in a cookie and a hash"
```

---

### Task 12: Signing in with GitHub

**Files:**
- Create: `site/src/lib/auth/github.ts`, `site/src/app/api/auth/github/route.ts`, `site/src/app/api/auth/github/callback/route.ts`, `site/src/app/api/auth/signout/route.ts`, `site/src/app/api/v1/me/route.ts`, `site/src/components/account-menu.tsx`
- Modify: `site/src/lib/layout.shared.tsx`
- Test: `site/src/lib/auth/github.test.ts`

**Interfaces:**
- Consumes: `githubOAuth()`, `publicUrl()`, `upsertGithubUser`, `createSession`, `destroySession`, `currentUser`.
- Produces: `authorizeUrl(state: string): string`, `exchangeCode(code: string): Promise<string>`, `readAccount(token: string): Promise<{ id: number; login: string; name: string | null; avatarUrl: string | null }>`, `STATE_COOKIE = "cubeship_oauth_state"`; `GET /api/v1/me` returning `{ user: SessionUser | null }`.

- [ ] **Step 1: Write the failing test**

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { authorizeUrl, exchangeCode, readAccount } from "./github";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("authorizeUrl", () => {
  it("asks for no scope at all and carries the state", () => {
    vi.stubEnv("GITHUB_CLIENT_ID", "abc");
    vi.stubEnv("GITHUB_CLIENT_SECRET", "shh");
    vi.stubEnv("SITE_URL", "https://cubeship.dev");

    const url = new URL(authorizeUrl("s1"));

    expect(url.host).toBe("github.com");
    expect(url.searchParams.get("client_id")).toBe("abc");
    expect(url.searchParams.get("state")).toBe("s1");
    expect(url.searchParams.get("scope")).toBe("");
    expect(url.searchParams.get("redirect_uri")).toBe("https://cubeship.dev/api/auth/github/callback");
  });
});

describe("exchangeCode", () => {
  it("returns the token", async () => {
    vi.stubEnv("GITHUB_CLIENT_ID", "abc");
    vi.stubEnv("GITHUB_CLIENT_SECRET", "shh");
    vi.stubGlobal("fetch", vi.fn(async () => Response.json({ access_token: "t" })));

    expect(await exchangeCode("c")).toBe("t");
  });

  it("throws when GitHub refuses", async () => {
    vi.stubEnv("GITHUB_CLIENT_ID", "abc");
    vi.stubEnv("GITHUB_CLIENT_SECRET", "shh");
    vi.stubGlobal("fetch", vi.fn(async () => Response.json({ error: "bad_verification_code" })));

    await expect(exchangeCode("c")).rejects.toThrow(/bad_verification_code/);
  });
});

describe("readAccount", () => {
  it("keeps only the four fields we store", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ id: 5, login: "lucas", name: "Lucas", avatar_url: "u", email: "x@y.z" })),
    );

    expect(await readAccount("t")).toEqual({ id: 5, login: "lucas", name: "Lucas", avatarUrl: "u" });
  });
});
```

Run: `cd site && pnpm test -- github` — expected FAIL.

- [ ] **Step 2: Write `github.ts`**

```ts
import { githubOAuth, publicUrl } from "@/lib/env";

export const STATE_COOKIE = "cubeship_oauth_state";

function redirectUri(): string {
  return `${publicUrl()}/api/auth/github/callback`;
}

// No scope: a template author's public identity is all this needs, and a
// token with reach is a liability we would then have to keep.
export function authorizeUrl(state: string): string {
  const url = new URL("https://github.com/login/oauth/authorize");
  url.searchParams.set("client_id", githubOAuth().clientId);
  url.searchParams.set("redirect_uri", redirectUri());
  url.searchParams.set("scope", "");
  url.searchParams.set("state", state);
  return url.toString();
}

export async function exchangeCode(code: string): Promise<string> {
  const { clientId, clientSecret } = githubOAuth();
  const response = await fetch("https://github.com/login/oauth/access_token", {
    method: "POST",
    headers: { accept: "application/json", "content-type": "application/json" },
    body: JSON.stringify({ client_id: clientId, client_secret: clientSecret, code, redirect_uri: redirectUri() }),
  });

  const body = (await response.json()) as { access_token?: string; error?: string };
  if (!body.access_token) throw new Error(body.error ?? "GitHub returned no token");
  return body.access_token;
}

export async function readAccount(token: string): Promise<{
  id: number;
  login: string;
  name: string | null;
  avatarUrl: string | null;
}> {
  const response = await fetch("https://api.github.com/user", {
    headers: { authorization: `Bearer ${token}`, accept: "application/vnd.github+json" },
  });
  if (!response.ok) throw new Error(`GitHub answered ${response.status}`);

  const body = (await response.json()) as { id: number; login: string; name?: string; avatar_url?: string };
  return { id: body.id, login: body.login, name: body.name ?? null, avatarUrl: body.avatar_url ?? null };
}
```

- [ ] **Step 3: Write the four routes**

`api/auth/github/route.ts`:

```ts
import { randomBytes } from "node:crypto";
import { cookies } from "next/headers";
import { authorizeUrl, STATE_COOKIE } from "@/lib/auth/github";
import { route } from "@/lib/http";

export const GET = route(async (request) => {
  const state = randomBytes(16).toString("base64url");
  const next = new URL(request.url).searchParams.get("next") ?? "/templates";

  const jar = await cookies();
  jar.set(STATE_COOKIE, `${state}:${next}`, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 600,
  });

  return Response.redirect(authorizeUrl(state), 302);
});
```

`api/auth/github/callback/route.ts`:

```ts
import { cookies } from "next/headers";
import { exchangeCode, readAccount, STATE_COOKIE } from "@/lib/auth/github";
import { createSession } from "@/lib/auth/session";
import { publicUrl } from "@/lib/env";
import { HttpError, route } from "@/lib/http";
import { upsertGithubUser } from "@/lib/users";

export const GET = route(async (request) => {
  const url = new URL(request.url);
  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");

  const jar = await cookies();
  const stored = jar.get(STATE_COOKIE)?.value;
  jar.delete(STATE_COOKIE);

  const [expected, next = "/templates"] = (stored ?? "").split(":");
  // A mismatch is somebody else starting the flow, not a slip.
  if (!code || !state || !expected || state !== expected) {
    throw new HttpError(400, "bad_state", "that sign-in did not start here; try again");
  }

  const token = await exchangeCode(code);
  const account = await readAccount(token);
  // The token has done its one job. Keeping it would be holding a
  // credential with nothing to spend it on.
  const user = await upsertGithubUser(account);
  await createSession(user.id);

  return Response.redirect(new URL(next, publicUrl()).toString(), 302);
});
```

`api/auth/signout/route.ts`:

```ts
import { destroySession } from "@/lib/auth/session";
import { json, route } from "@/lib/http";

export const POST = route(async () => {
  await destroySession();
  return json({ ok: true });
});
```

`api/v1/me/route.ts`:

```ts
import { currentUser } from "@/lib/auth/session";
import { json, route } from "@/lib/http";

// Never cached: it is the one answer that differs per request.
export const dynamic = "force-dynamic";

export const GET = route(async () => json({ user: await currentUser() }));
```

- [ ] **Step 4: Write the header's account menu**

`src/components/account-menu.tsx` is a client component. It fetches `/api/v1/me` on mount, shows a "Sign in" link to `/api/auth/github?next=` the current path when there is no user, and the avatar with a small menu otherwise: the author's templates, their drafts, sign out. Sign out posts to `/api/auth/signout` and reloads.

```tsx
"use client";

import { useEffect, useState } from "react";

type Me = { id: number; login: string; avatarUrl: string | null; role: string } | null;

export function AccountMenu() {
  const [me, setMe] = useState<Me>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    fetch("/api/v1/me")
      .then((response) => response.json())
      .then((body) => setMe(body.user))
      .catch(() => setMe(null));
  }, []);

  if (!me) {
    return (
      <a
        href={`/api/auth/github?next=${encodeURIComponent(typeof window === "undefined" ? "/templates" : window.location.pathname)}`}
        className="label text-fd-muted-foreground hover:text-fd-foreground"
      >
        Sign in
      </a>
    );
  }

  return (
    <div className="relative">
      <button type="button" onClick={() => setOpen(!open)} aria-label={`Account: ${me.login}`}>
        {me.avatarUrl ? (
          <img src={me.avatarUrl} alt="" className="size-7 border border-fd-border" />
        ) : (
          <span className="label">{me.login}</span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 z-50 mt-2 w-44 border border-fd-border bg-fd-background p-2 text-sm">
          <a href={`/u/${me.login}`} className="block px-2 py-1 hover:text-fd-foreground">
            My templates
          </a>
          <a href="/me/templates" className="block px-2 py-1 hover:text-fd-foreground">
            Drafts
          </a>
          <button
            type="button"
            className="block w-full px-2 py-1 text-left hover:text-fd-foreground"
            onClick={async () => {
              await fetch("/api/auth/signout", { method: "POST" });
              window.location.reload();
            }}
          >
            Sign out
          </button>
        </div>
      )}
    </div>
  );
}
```

In `src/lib/layout.shared.tsx`, add `{ type: "custom", secondary: true, children: <AccountMenu /> }` to the nav links, beside the existing GitHub stars entry.

- [ ] **Step 5: Sign in for real**

Create a GitHub OAuth app with callback `http://localhost:3002/api/auth/github/callback`, then:

```bash
cd site
DATABASE_URL=postgres://site:site@localhost:5434/site GITHUB_CLIENT_ID=... GITHUB_CLIENT_SECRET=... ADMIN_LOGINS=yourlogin pnpm dev
```

Visit `http://localhost:3002/api/auth/github`, complete the flow, and confirm the avatar appears in the header and `/api/v1/me` returns your login and the admin role.

- [ ] **Step 6: Format, lint, commit**

```bash
cd site && pnpm run format && pnpm run lint && pnpm run typecheck && pnpm test
cd .. && git add site/src
git commit -m "Sign template authors in with GitHub, and keep nothing after"
```

---

## Phase 3: Authoring and publishing

Phase 3 ends with a signed-in author writing a template in a validating editor and publishing version 1.

### Task 13: The bucket

**Files:**
- Create: `site/src/lib/storage.ts`, `site/src/app/i/[...key]/route.ts`
- Test: `site/src/lib/storage.test.ts`
- Modify: `site/package.json`

**Interfaces:**
- Consumes: `s3Config()`.
- Produces: `type ObjectClient = { put(key: string, body: Uint8Array, contentType: string): Promise<void>; get(key: string): Promise<{ body: Uint8Array; contentType: string }> }`, `s3Client(): ObjectClient`, `putImage(bytes: Uint8Array, client?: ObjectClient): Promise<string>`, `readImage(key: string, client?: ObjectClient)`, `IMAGE_LIMIT = 2 * 1024 * 1024`.

- [ ] **Step 1: Add the dependencies**

```bash
cd site && pnpm add @aws-sdk/client-s3 sharp
```

- [ ] **Step 2: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { type ObjectClient, putImage } from "./storage";

function memoryClient(): ObjectClient & { store: Map<string, { body: Uint8Array; contentType: string }> } {
  const store = new Map<string, { body: Uint8Array; contentType: string }>();
  return {
    store,
    async put(key, body, contentType) {
      store.set(key, { body, contentType });
    },
    async get(key) {
      const found = store.get(key);
      if (!found) throw new Error("missing");
      return found;
    },
  };
}

// A 2x2 PNG, so the test needs no fixture file on disk.
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAACp8Z5+AAAAFUlEQVR4nGP8z8DAwMDAxMDAwAAABQkAsQZbBQAAAABJRU5ErkJggg==",
  "base64",
);

describe("putImage", () => {
  it("re-encodes to WebP under a generated key", async () => {
    const client = memoryClient();
    const key = await putImage(png, client);

    expect(key).toMatch(/^templates\/[0-9a-f-]{36}\.webp$/);
    expect(client.store.get(key)?.contentType).toBe("image/webp");
    expect(client.store.get(key)?.body.subarray(8, 12).toString()).toBe("WEBP");
  });

  it("refuses what is not an image", async () => {
    await expect(putImage(Buffer.from("not an image"), memoryClient())).rejects.toThrow(/image/);
  });

  it("refuses a file over the limit", async () => {
    await expect(putImage(Buffer.alloc(2 * 1024 * 1024 + 1), memoryClient())).rejects.toThrow(/2 MB/);
  });
});
```

Run: `cd site && pnpm test -- storage` — expected FAIL.

- [ ] **Step 3: Write `storage.ts`**

```ts
import { randomUUID } from "node:crypto";
import { GetObjectCommand, PutObjectCommand, S3Client } from "@aws-sdk/client-s3";
import sharp from "sharp";
import { s3Config } from "./env";
import { HttpError } from "./http";

export const IMAGE_LIMIT = 2 * 1024 * 1024;
// One size, so every card in the catalog weighs the same.
const WIDTH = 1200;
const HEIGHT = 675;

export type ObjectClient = {
  put(key: string, body: Uint8Array, contentType: string): Promise<void>;
  get(key: string): Promise<{ body: Uint8Array; contentType: string }>;
};

export function s3Client(): ObjectClient {
  const config = s3Config();
  const client = new S3Client({
    endpoint: config.endpoint,
    region: config.region,
    forcePathStyle: config.pathStyle,
    credentials: { accessKeyId: config.accessKeyId, secretAccessKey: config.secretAccessKey },
  });

  return {
    async put(key, body, contentType) {
      await client.send(
        new PutObjectCommand({ Bucket: config.bucket, Key: key, Body: body, ContentType: contentType }),
      );
    },
    async get(key) {
      const response = await client.send(new GetObjectCommand({ Bucket: config.bucket, Key: key }));
      const body = new Uint8Array(await (response.Body as { transformToByteArray(): Promise<Uint8Array> }).transformToByteArray());
      return { body, contentType: response.ContentType ?? "application/octet-stream" };
    },
  };
}

// Never stored as uploaded: an image taken verbatim is an attack surface
// and an unbounded payload.
export async function putImage(bytes: Uint8Array, client: ObjectClient = s3Client()): Promise<string> {
  if (bytes.byteLength > IMAGE_LIMIT) throw new HttpError(413, "too_large", "a photo is at most 2 MB");

  let webp: Buffer;
  try {
    webp = await sharp(bytes).rotate().resize(WIDTH, HEIGHT, { fit: "cover" }).webp({ quality: 82 }).toBuffer();
  } catch {
    throw new HttpError(415, "not_an_image", "that file is not an image this can read");
  }

  const key = `templates/${randomUUID()}.webp`;
  await client.put(key, webp, "image/webp");
  return key;
}

export async function readImage(key: string, client: ObjectClient = s3Client()) {
  return client.get(key);
}
```

- [ ] **Step 4: Write the serving route**

`src/app/i/[...key]/route.ts`:

```ts
import { HttpError, route } from "@/lib/http";
import { readImage } from "@/lib/storage";

// Served through here rather than from a public bucket: the bucket stays
// private and this is the only way in.
export const GET = route(async (_request, context) => {
  const { key } = (await context.params) as unknown as { key: string[] };
  const path = key.join("/");
  if (!path.startsWith("templates/") || path.includes("..")) {
    throw new HttpError(404, "not_found", "no such image");
  }

  try {
    const { body, contentType } = await readImage(path);
    return new Response(body, {
      headers: { "content-type": contentType, "cache-control": "public, max-age=31536000, immutable" },
    });
  } catch {
    throw new HttpError(404, "not_found", "no such image");
  }
});
```

- [ ] **Step 5: Run the tests, format, commit**

```bash
cd site && pnpm test -- storage && pnpm run format && pnpm run typecheck
cd .. && git add site/src/lib/storage.ts site/src/app/i site/package.json site/pnpm-lock.yaml
git commit -m "Re-encode every photo and keep the bucket private"
```

---

### Task 14: Drafts, metadata and publishing a version

**Files:**
- Create: `site/src/lib/templates/slug.ts`, `site/src/lib/templates/publish.ts`, `site/src/lib/templates/queries.ts`
- Create: `site/src/app/api/v1/templates/route.ts`, `site/src/app/api/v1/templates/[slug]/route.ts`, `site/src/app/api/v1/templates/[slug]/versions/route.ts`, `site/src/app/api/v1/templates/[slug]/image/route.ts`
- Test: `site/src/lib/templates/slug.db.test.ts`, `site/src/lib/templates/publish.db.test.ts`

**Interfaces:**
- Consumes: `db()`, the tables, `requireUser`, `requireAdmin`, `validateTemplate`, `putImage`, `HttpError`, `route`.
- Produces: `freeSlug(name: string): Promise<string>`; `createDraft(user, input: { name: string; summary: string; tags: string[] }): Promise<{ slug: string }>`; `updateMetadata(user, slug, patch: { name?: string; summary?: string; tags?: string[]; imageKey?: string; status?: "draft" | "published" | "unlisted" }): Promise<void>`; `publishVersion(user, slug, input: { source: string; notes?: string }): Promise<{ number: number }>`; `ownedTemplate(user, slug)`; `templateBySlug(slug)`; `versionsOf(templateId)`; `versionOf(templateId, number)`.

- [ ] **Step 1: Write the failing tests**

```ts
// publish.db.test.ts
import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { createDraft, publishVersion, updateMetadata } from "./publish";
import { templateBySlug, versionsOf } from "./queries";

const good = `version: 1
project: demo
apps:
  - key: web
    image: nginx
    tag: "1"
    health: /
    port: 80
    limits: { cpu: 1, memory: 512Mi }
    domains: []
`;

async function author() {
  const handle = await withDatabase();
  const [user] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
  return { id: user.id, login: user.login, name: null, avatarUrl: null, role: "user" as const };
}

describe.skipIf(!testDatabaseUrl)("publishing", () => {
  it("starts as a draft with no version", async () => {
    const user = await author();
    const { slug } = await createDraft(user, { name: "My Stack", summary: "s", tags: ["analytics"] });
    const template = await templateBySlug(slug);

    expect(slug).toBe("my-stack");
    expect(template?.status).toBe("draft");
    expect(template?.currentVersionId).toBeNull();
  });

  it("numbers versions from one and moves the pointer", async () => {
    const user = await author();
    const { slug } = await createDraft(user, { name: "s", summary: "s", tags: [] });

    expect((await publishVersion(user, slug, { source: good })).number).toBe(1);
    expect((await publishVersion(user, slug, { source: good, notes: "again" })).number).toBe(2);

    const template = await templateBySlug(slug);
    const versions = await versionsOf(template?.id ?? 0);
    expect(versions).toHaveLength(2);
    expect(template?.status).toBe("published");
  });

  it("refuses a file with an error, with the diagnostics", async () => {
    const user = await author();
    const { slug } = await createDraft(user, { name: "s", summary: "s", tags: [] });

    await expect(publishVersion(user, slug, { source: "version: 1\napps: []\n" })).rejects.toMatchObject({
      status: 422,
      code: "invalid_template",
    });
  });

  it("refuses somebody else's template", async () => {
    const user = await author();
    const { slug } = await createDraft(user, { name: "s", summary: "s", tags: [] });
    const other = { ...user, id: user.id + 1 };

    await expect(updateMetadata(other, slug, { name: "mine now" })).rejects.toMatchObject({ status: 404 });
  });

  it("gives a second template of the same name its own slug", async () => {
    const user = await author();
    await createDraft(user, { name: "Stack", summary: "s", tags: [] });

    expect((await createDraft(user, { name: "Stack", summary: "s", tags: [] })).slug).toBe("stack-2");
  });
});
```

Run: `cd site && SITE_TEST_DATABASE_URL=... pnpm test -- publish.db` — expected FAIL.

- [ ] **Step 2: Write `slug.ts`**

```ts
import { eq } from "drizzle-orm";
import { db } from "@/db/client";
import { templates } from "@/db/schema";

// A slug is permanent once published, so it is worth getting a readable
// one at creation and never touching it again.
export async function freeSlug(name: string): Promise<string> {
  const base =
    name
      .toLowerCase()
      .normalize("NFD")
      .replace(/[̀-ͯ]/g, "")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48) || "template";

  for (let attempt = 1; attempt < 100; attempt++) {
    const candidate = attempt === 1 ? base : `${base}-${attempt}`;
    const [taken] = await db().select({ id: templates.id }).from(templates).where(eq(templates.slug, candidate)).limit(1);
    if (!taken) return candidate;
  }

  throw new Error(`no free slug for "${name}"`);
}
```

- [ ] **Step 3: Write `queries.ts` (the reads this task needs)**

```ts
import { and, desc, eq } from "drizzle-orm";
import { db } from "@/db/client";
import { templates, templateVersions, users } from "@/db/schema";

export async function templateBySlug(slug: string) {
  const [row] = await db().select().from(templates).where(eq(templates.slug, slug)).limit(1);
  return row;
}

export async function templateWithAuthor(slug: string) {
  const [row] = await db()
    .select({ template: templates, author: { login: users.login, name: users.name, avatarUrl: users.avatarUrl } })
    .from(templates)
    .innerJoin(users, eq(users.id, templates.authorId))
    .where(eq(templates.slug, slug))
    .limit(1);
  return row;
}

export async function versionsOf(templateId: number) {
  return db()
    .select({ id: templateVersions.id, number: templateVersions.number, notes: templateVersions.notes, createdAt: templateVersions.createdAt })
    .from(templateVersions)
    .where(eq(templateVersions.templateId, templateId))
    .orderBy(desc(templateVersions.number));
}

export async function versionOf(templateId: number, number: number) {
  const [row] = await db()
    .select()
    .from(templateVersions)
    .where(and(eq(templateVersions.templateId, templateId), eq(templateVersions.number, number)))
    .limit(1);
  return row;
}

export async function currentVersion(templateId: number) {
  const [row] = await db()
    .select()
    .from(templateVersions)
    .where(eq(templateVersions.templateId, templateId))
    .orderBy(desc(templateVersions.number))
    .limit(1);
  return row;
}
```

- [ ] **Step 4: Write `publish.ts`**

```ts
import { and, eq, gte, sql } from "drizzle-orm";
import { db } from "@/db/client";
import { templates, templateVersions } from "@/db/schema";
import type { SessionUser } from "@/lib/auth/session";
import { HttpError } from "@/lib/http";
import { validateTemplate } from "@/lib/template";
import { freeSlug } from "./slug";

const DRAFTS_PER_DAY = 20;

export async function createDraft(
  user: SessionUser,
  input: { name: string; summary: string; tags: string[] },
): Promise<{ slug: string }> {
  const since = new Date(Date.now() - 24 * 60 * 60 * 1000);
  const [{ count }] = await db()
    .select({ count: sql<number>`count(*)::int` })
    .from(templates)
    .where(and(eq(templates.authorId, user.id), gte(templates.createdAt, since)));
  if (count >= DRAFTS_PER_DAY) {
    throw new HttpError(429, "too_many", "that is a lot of templates for one day; try again tomorrow");
  }

  const slug = await freeSlug(input.name);
  await db().insert(templates).values({
    slug,
    authorId: user.id,
    name: input.name,
    summary: input.summary,
    tags: input.tags,
  });

  return { slug };
}

// Ownership is a read that 404s rather than 403s only for a template the
// caller cannot see at all; an author's own template answers normally.
export async function ownedTemplate(user: SessionUser, slug: string) {
  const [row] = await db().select().from(templates).where(eq(templates.slug, slug)).limit(1);
  if (!row || (row.authorId !== user.id && user.role !== "admin")) {
    throw new HttpError(404, "not_found", "no template of that name");
  }
  return row;
}

export async function updateMetadata(
  user: SessionUser,
  slug: string,
  patch: { name?: string; summary?: string; tags?: string[]; imageKey?: string; status?: "draft" | "published" | "unlisted" },
): Promise<void> {
  const template = await ownedTemplate(user, slug);
  if (patch.status === "published" && !template.currentVersionId) {
    throw new HttpError(409, "no_version", "publish a version before publishing the template");
  }

  await db()
    .update(templates)
    .set({ ...patch, updatedAt: new Date() })
    .where(eq(templates.id, template.id));
}

export async function publishVersion(
  user: SessionUser,
  slug: string,
  input: { source: string; notes?: string },
): Promise<{ number: number }> {
  const template = await ownedTemplate(user, slug);
  const result = validateTemplate(input.source);
  if (!result.ok || !result.manifest) {
    throw new HttpError(422, "invalid_template", "that template has errors", { diagnostics: result.diagnostics });
  }

  return db().transaction(async (tx) => {
    const [{ highest }] = await tx
      .select({ highest: sql<number>`coalesce(max(number), 0)::int` })
      .from(templateVersions)
      .where(eq(templateVersions.templateId, template.id));
    const number = highest + 1;

    const [version] = await tx
      .insert(templateVersions)
      .values({
        templateId: template.id,
        number,
        manifest: result.manifest,
        source: input.source,
        schemaVersion: result.manifest.schema_version,
        notes: input.notes ?? null,
      })
      .returning();

    // The first version is also what takes a draft public.
    await tx
      .update(templates)
      .set({
        currentVersionId: version.id,
        status: template.status === "draft" ? "published" : template.status,
        updatedAt: new Date(),
      })
      .where(eq(templates.id, template.id));

    return { number };
  });
}
```

- [ ] **Step 5: Write the four routes**

`api/v1/templates/route.ts` handles `POST`: `requireUser`, read a JSON body validated by a small Zod schema (`name` 3 to 60 characters, `summary` 10 to 160, `tags` at most 5 of at most 20 characters each), call `createDraft`, return `{ slug }` with 201.

```ts
import { z } from "zod";
import { requireUser } from "@/lib/auth/guard";
import { HttpError, json, route } from "@/lib/http";
import { createDraft } from "@/lib/templates/publish";

const body = z.object({
  name: z.string().min(3).max(60),
  summary: z.string().min(10).max(160),
  tags: z.array(z.string().min(2).max(20)).max(5).default([]),
});

export const POST = route(async (request) => {
  const user = await requireUser();
  const parsed = body.safeParse(await request.json());
  if (!parsed.success) throw new HttpError(422, "invalid_body", parsed.error.issues[0].message);

  return json(await createDraft(user, parsed.data), { status: 201 });
});
```

`api/v1/templates/[slug]/route.ts` handles `PATCH` with the same field rules plus `imageKey` and `status`, and `DELETE`, which sets the status to `removed` rather than deleting rows, so the slug is never reused.

`api/v1/templates/[slug]/versions/route.ts` handles `POST` with `{ source, notes }`, calling `publishVersion`; on an `HttpError` carrying diagnostics the wrapper already renders them.

`api/v1/templates/[slug]/image/route.ts` handles `POST`: `requireUser`, `ownedTemplate`, read `await request.formData()`, take the `file` entry, `putImage`, then `updateMetadata` with the key, returning `{ key, url: "/i/" + key }`.

- [ ] **Step 6: Run the tests and check by hand**

```bash
cd site && SITE_TEST_DATABASE_URL=postgres://site:site@localhost:5434/site pnpm test
```

With the dev server signed in, create and publish one through the API:

```bash
curl -sS -X POST http://localhost:3002/api/v1/templates -H 'content-type: application/json' \
  -b cookies.txt -d '{"name":"Umami","summary":"Privacy-friendly web analytics","tags":["analytics"]}'
```

- [ ] **Step 7: Format, lint, commit**

```bash
cd site && pnpm run format && pnpm run lint && pnpm run typecheck && pnpm test
cd .. && git add site/src
git commit -m "Publish a template as a numbered, immutable version"
```

---

### Task 15: The editor

**Files:**
- Create: `site/src/components/templates/editor.tsx`, `site/src/components/templates/preview.tsx`, `site/src/components/templates/problems.tsx`, `site/src/components/templates/form.tsx`
- Create: `site/src/app/templates/new/page.tsx`, `site/src/app/templates/[slug]/edit/page.tsx`
- Modify: `site/package.json`
- Test: `site/src/components/templates/preview.test.ts` (the summary strings, not the DOM)

**Interfaces:**
- Consumes: `validateTemplate`, `type Diagnostic`, `type NormalizedManifest`, the API from Task 14.
- Produces: `<TemplateEditor value onChange diagnostics />`, `<Problems diagnostics onJump />`, `<Preview manifest />`, `<TemplateForm mode="new" | "edit" template? />`, and the two sentence helpers the preview renders and the test calls directly: `describeApp(app: NormalizedApp, inputs: Manifest["inputs"]): string` and `describeDatabase(database: NormalizedManifest["databases"][number]): string`.

- [ ] **Step 1: Add the editor dependencies**

```bash
cd site && pnpm add codemirror @codemirror/lang-yaml @codemirror/lint @codemirror/state @codemirror/view
```

- [ ] **Step 2: Write the failing test for the preview's sentences**

```ts
import { describe, expect, it } from "vitest";
import { validateTemplate } from "@/lib/template";
import { describeApp, describeDatabase } from "./preview";

const { manifest } = validateTemplate(`version: 1
project: umami
inputs:
  - key: domain
    type: domain
    label: Where it answers
databases:
  - key: db
    engine: postgres
    version: "18"
apps:
  - key: web
    image: nginx
    tag: "1"
    port: 3000
    domains:
      - host: \${input.domain}
    attach:
      - database: db
`);

describe("the preview's sentences", () => {
  it("says what an app is and where it answers", () => {
    if (!manifest) throw new Error("the fixture does not validate");

    expect(describeApp(manifest.apps[0], manifest.inputs)).toBe(
      "nginx:1 on port 3000, at the domain from “Where it answers”, reachable inside as cubeship-umami-production-web:3000",
    );
  });

  it("says what a database is", () => {
    if (!manifest) throw new Error("the fixture does not validate");

    expect(describeDatabase(manifest.databases[0])).toBe("postgres 18, named db, on port 5432");
  });
});
```

Run: `cd site && pnpm test -- preview` — expected FAIL.

- [ ] **Step 3: Write `preview.tsx`**

It exports the two description helpers as plain functions, and a component that lists apps, databases, stores, attachments and inputs from a `NormalizedManifest`, using `hud-frame` blocks and the mono `label` class. The helpers take the normalized shapes so they need no React to be tested. `describeApp` reads the input's label for a domain reference by taking the inputs list as its second argument.

- [ ] **Step 4: Write `editor.tsx`**

```tsx
"use client";

import { yaml } from "@codemirror/lang-yaml";
import { type Diagnostic as CmDiagnostic, linter, lintGutter } from "@codemirror/lint";
import { EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { basicSetup } from "codemirror";
import { useEffect, useRef } from "react";
import type { Diagnostic } from "@/lib/template";

// The same module the server publishes with, so what the author sees is
// what the publish will say.
export function TemplateEditor({
  value,
  onChange,
  diagnose,
}: {
  value: string;
  onChange: (next: string) => void;
  diagnose: (source: string) => Diagnostic[];
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(null);

  useEffect(() => {
    if (!host.current || view.current) return;

    view.current = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          yaml(),
          lintGutter(),
          linter(
            (editor): CmDiagnostic[] =>
              diagnose(editor.state.doc.toString()).map((found) => ({
                from: found.range?.start.offset ?? 0,
                to: found.range?.end.offset ?? Math.min(1, editor.state.doc.length),
                severity: found.severity === "info" ? "info" : found.severity,
                message: found.hint ? `${found.message} — ${found.hint}` : found.message,
                source: found.code,
              })),
            { delay: 300 },
          ),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) onChange(update.state.doc.toString());
          }),
          EditorView.theme({
            "&": { fontSize: "13px", backgroundColor: "transparent" },
            ".cm-gutters": { backgroundColor: "transparent", borderRight: "1px solid var(--color-fd-border)" },
          }),
        ],
      }),
    });

    return () => {
      view.current?.destroy();
      view.current = null;
    };
  }, [diagnose, onChange, value]);

  return <div ref={host} className="hud-frame min-h-[28rem] border border-fd-border" />;
}
```

CodeMirror is imported only by this component, and this component is imported only by `/templates/new` and `/templates/[slug]/edit`, so it never reaches the catalog's or the landing page's bundle. Keep it that way: nothing shared may import it.

- [ ] **Step 5: Write `problems.tsx` and `form.tsx`**

`Problems` lists diagnostics with severity, code and message, each a button that scrolls the editor to the range. `TemplateForm` is the client component that owns the state: name, summary, tags, the photo file, the source text, and the last validation result. It calls `validateTemplate` directly in the browser for feedback, and on submit does the API calls in order: create the draft if there is no slug yet, upload the photo if one was chosen, then post the version. On a 422 it renders the server's diagnostics beside its own, because the server is the authority.

- [ ] **Step 6: Write the two pages**

`/templates/new/page.tsx` is a server component: `currentUser()`, redirect to `/api/auth/github?next=/templates/new` when there is none, otherwise render `<TemplateForm mode="new" />`. `/templates/[slug]/edit/page.tsx` loads the template and its current version through `templateWithAuthor` and `currentVersion`, refuses anyone but the author or an admin with `notFound()`, and renders `<TemplateForm mode="edit" ... />` seeded with the stored YAML.

Both pages carry `export const dynamic = "force-dynamic"`, because they read the session.

- [ ] **Step 7: Publish one by hand**

With the dev server signed in, open `http://localhost:3002/templates/new`, paste the fixture, confirm that removing `health` shows a warning and that changing `${input.domain}` to a literal host shows `domain.literal` on that line, then publish and confirm version 1 exists.

- [ ] **Step 8: Format, lint, commit**

```bash
cd site && pnpm run format && pnpm run lint && pnpm run typecheck && pnpm test && pnpm run build
cd .. && git add site/src site/package.json site/pnpm-lock.yaml
git commit -m "Write a template in an editor that points at the mistake"
```

---

## Phase 4: The catalog, likes and comments

Phase 4 ends with the feature complete: a browsable catalog, a detail page an instance can read as JSON, likes, comments, reports and an admin queue.

### Task 16: The catalog

**Files:**
- Modify: `site/src/lib/templates/queries.ts`
- Create: `site/src/app/api/v1/templates/route.ts` (add `GET` beside the existing `POST`)
- Create: `site/src/app/templates/page.tsx`, `site/src/app/templates/layout.tsx`, `site/src/components/templates/card.tsx`, `site/src/components/templates/filters.tsx`
- Test: `site/src/lib/templates/catalog.db.test.ts`

**Interfaces:**
- Consumes: `db()`, the tables.
- Produces: `type CatalogRow = { slug: string; name: string; summary: string; imageKey: string | null; tags: string[]; likesCount: number; author: { login: string; avatarUrl: string | null }; creates: { apps: number; databases: number; engines: string[] } }`, `listTemplates(options: { q?: string; tag?: string; sort?: "recent" | "likes"; cursor?: string; limit?: number }): Promise<{ rows: CatalogRow[]; nextCursor: string | null }>`, `encodeCursor`/`decodeCursor`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { listTemplates } from "./queries";
import { createDraft, publishVersion } from "./publish";

const source = `version: 1
project: demo
databases:
  - key: db
    engine: postgres
    version: "18"
apps:
  - key: web
    image: nginx
    tag: "1"
    health: /
    port: 80
    limits: { cpu: 1, memory: 512Mi }
`;

describe.skipIf(!testDatabaseUrl)("listTemplates", () => {
  it("lists what is published, newest first, with what it creates", async () => {
    const handle = await withDatabase();
    const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
    const user = { id: row.id, login: row.login, name: null, avatarUrl: null, role: "user" as const };

    const first = await createDraft(user, { name: "Alpha", summary: "the first one", tags: ["analytics"] });
    await publishVersion(user, first.slug, { source });
    await createDraft(user, { name: "Beta", summary: "still a draft", tags: [] });

    const { rows } = await listTemplates({});

    expect(rows.map((r) => r.slug)).toEqual(["alpha"]);
    expect(rows[0].creates).toEqual({ apps: 1, databases: 1, engines: ["postgres"] });
    expect(rows[0].author.login).toBe("lucas");
  });

  it("filters by tag and by text, and pages", async () => {
    const handle = await withDatabase();
    const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
    const user = { id: row.id, login: row.login, name: null, avatarUrl: null, role: "user" as const };

    for (const name of ["One", "Two", "Three"]) {
      const { slug } = await createDraft(user, { name, summary: `${name} summary`, tags: ["web"] });
      await publishVersion(user, slug, { source });
    }

    expect((await listTemplates({ tag: "web" })).rows).toHaveLength(3);
    expect((await listTemplates({ tag: "none" })).rows).toHaveLength(0);
    expect((await listTemplates({ q: "two" })).rows.map((r) => r.slug)).toEqual(["two"]);

    const page = await listTemplates({ limit: 2 });
    expect(page.rows).toHaveLength(2);
    expect(page.nextCursor).not.toBeNull();
    expect((await listTemplates({ limit: 2, cursor: page.nextCursor ?? "" })).rows).toHaveLength(1);
  });
});
```

- [ ] **Step 2: Add `listTemplates` to `queries.ts`**

Extend the import at the top of the file to `import { and, desc, eq, sql } from "drizzle-orm";` — the paging predicates are raw SQL tuples.

```ts
// Keyset paging, not offset: the catalog is sorted by something that
// changes, and an offset page would repeat or skip rows as it does.
export function encodeCursor(parts: [number, number]): string {
  return Buffer.from(parts.join(":")).toString("base64url");
}

export function decodeCursor(cursor: string): [number, number] | undefined {
  const [first, second] = Buffer.from(cursor, "base64url").toString().split(":").map(Number);
  return Number.isFinite(first) && Number.isFinite(second) ? [first, second] : undefined;
}

export async function listTemplates(options: {
  q?: string;
  tag?: string;
  sort?: "recent" | "likes";
  cursor?: string;
  limit?: number;
}) {
  const limit = Math.min(Math.max(options.limit ?? 24, 1), 48);
  const sort = options.sort ?? "recent";
  const after = options.cursor ? decodeCursor(options.cursor) : undefined;

  const conditions = [eq(templates.status, "published")];
  if (options.tag) conditions.push(sql`${templates.tags} @> array[${options.tag}]::text[]`);
  if (options.q) {
    const like = `%${options.q.toLowerCase()}%`;
    conditions.push(sql`(lower(${templates.name}) like ${like} or lower(${templates.summary}) like ${like})`);
  }
  if (after) {
    conditions.push(
      sort === "likes"
        ? sql`(${templates.likesCount}, ${templates.id}) < (${after[0]}, ${after[1]})`
        : sql`(extract(epoch from ${templates.createdAt})::bigint, ${templates.id}) < (${after[0]}, ${after[1]})`,
    );
  }

  const rows = await db()
    .select({
      id: templates.id,
      slug: templates.slug,
      name: templates.name,
      summary: templates.summary,
      imageKey: templates.imageKey,
      tags: templates.tags,
      likesCount: templates.likesCount,
      createdAt: templates.createdAt,
      manifest: templateVersions.manifest,
      login: users.login,
      avatarUrl: users.avatarUrl,
    })
    .from(templates)
    .innerJoin(users, eq(users.id, templates.authorId))
    .leftJoin(templateVersions, eq(templateVersions.id, templates.currentVersionId))
    .where(and(...conditions))
    .orderBy(
      sort === "likes" ? desc(templates.likesCount) : desc(templates.createdAt),
      desc(templates.id),
    )
    .limit(limit + 1);

  const page = rows.slice(0, limit);
  const last = page.at(-1);
  const nextCursor =
    rows.length > limit && last
      ? encodeCursor([sort === "likes" ? last.likesCount : Math.floor(last.createdAt.getTime() / 1000), last.id])
      : null;

  return {
    rows: page.map((row) => {
      const manifest = (row.manifest ?? { apps: [], databases: [] }) as {
        apps: unknown[];
        databases: { engine: string }[];
      };
      return {
        slug: row.slug,
        name: row.name,
        summary: row.summary,
        imageKey: row.imageKey,
        tags: row.tags,
        likesCount: row.likesCount,
        author: { login: row.login, avatarUrl: row.avatarUrl },
        creates: {
          apps: manifest.apps.length,
          databases: manifest.databases.length,
          engines: [...new Set(manifest.databases.map((database) => database.engine))],
        },
      };
    }),
    nextCursor,
  };
}
```

- [ ] **Step 3: Add `GET` to the templates route**

```ts
export const GET = route(async (request) => {
  const params = new URL(request.url).searchParams;
  const sort = params.get("sort") === "likes" ? "likes" : "recent";

  const page = await listTemplates({
    q: params.get("q") ?? undefined,
    tag: params.get("tag") ?? undefined,
    sort,
    cursor: params.get("cursor") ?? undefined,
    limit: Number(params.get("limit")) || undefined,
  });

  return json(page, { headers: { "access-control-allow-origin": "*" } });
});
```

- [ ] **Step 4: Write the page and the card**

`/templates/page.tsx` is request-rendered (`export const dynamic = "force-dynamic"`, because it reads Postgres), calls `listTemplates` directly rather than through HTTP, and renders `<Filters />` plus a grid of `<TemplateCard />`. The card is a `hud-frame` block: the photo at 16 by 9 from `/i/<key>` with a `bg-grid` placeholder when there is none, the name, the summary, the author, the like count, and one mono line saying what it creates, for instance `2 apps · postgres`. `<Filters />` is a client component holding the search field, the tag chips and the sort toggle in the query string. `/templates/layout.tsx` reuses the home layout's nav and footer.

The empty state says that nothing is published yet and links to `/templates/new`.

- [ ] **Step 5: Run, check, commit**

```bash
cd site && SITE_TEST_DATABASE_URL=postgres://site:site@localhost:5434/site pnpm test && pnpm run format && pnpm run lint && pnpm run typecheck
cd .. && git add site/src
git commit -m "List the published templates, newest or most liked first"
```

---

### Task 17: The detail page and the manifest endpoint

**Files:**
- Create: `site/src/app/templates/[slug]/page.tsx`, `site/src/app/templates/[slug]/versions/[number]/page.tsx`
- Create: `site/src/app/api/v1/templates/[slug]/manifest/route.ts`
- Modify: `site/src/app/api/v1/templates/[slug]/route.ts` (add `GET`), `site/src/app/api/v1/templates/[slug]/versions/route.ts` (add `GET`)
- Create: `site/src/components/templates/source-block.tsx`
- Test: `site/src/app/api/v1/templates/[slug]/manifest/route.db.test.ts`

**Interfaces:**
- Consumes: `templateWithAuthor`, `versionsOf`, `versionOf`, `currentVersion`.
- Produces: `GET /api/v1/templates/{slug}` returning `{ template, author, version: { number, manifest, source, notes } , versions: number[] }`; `GET /api/v1/templates/{slug}/manifest[?version=n]` returning the manifest document alone.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { createDraft, publishVersion } from "@/lib/templates/publish";
import { GET } from "./route";

const source = `version: 1
project: demo
apps:
  - key: web
    image: nginx
    tag: "1"
    health: /
    port: 80
    limits: { cpu: 1, memory: 512Mi }
`;

async function seed() {
  const handle = await withDatabase();
  const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
  const user = { id: row.id, login: row.login, name: null, avatarUrl: null, role: "user" as const };
  const { slug } = await createDraft(user, { name: "Demo", summary: "a demo template", tags: [] });
  await publishVersion(user, slug, { source });
  await publishVersion(user, slug, { source: source.replace('"1"', '"2"') });
  return slug;
}

function request(slug: string, query = "") {
  return GET(new Request(`http://localhost/api/v1/templates/${slug}/manifest${query}`), {
    params: Promise.resolve({ slug }),
  });
}

describe.skipIf(!testDatabaseUrl)("GET /api/v1/templates/{slug}/manifest", () => {
  it("serves the newest version by default", async () => {
    const slug = await seed();
    const body = await (await request(slug)).json();

    expect(body.schema_version).toBe(1);
    expect(body.apps[0].source.tag).toBe("2");
  });

  it("serves a pinned version", async () => {
    const slug = await seed();
    const body = await (await request(slug, "?version=1")).json();

    expect(body.apps[0].source.tag).toBe("1");
  });

  it("404s for an unknown template and an unknown version", async () => {
    const slug = await seed();

    expect((await request("nope")).status).toBe(404);
    expect((await request(slug, "?version=9")).status).toBe(404);
  });

  it("allows cross-origin reads, because an instance will do one", async () => {
    const slug = await seed();

    expect((await request(slug)).headers.get("access-control-allow-origin")).toBe("*");
  });
});
```

- [ ] **Step 2: Write the manifest route**

```ts
import { HttpError, json, route } from "@/lib/http";
import { currentVersion, templateBySlug, versionOf } from "@/lib/templates/queries";

export const dynamic = "force-dynamic";

export const GET = route(async (request, context) => {
  const { slug } = await context.params;
  const template = await templateBySlug(slug);
  if (!template || template.status === "removed" || template.status === "draft") {
    throw new HttpError(404, "not_found", "no template of that name");
  }

  const pinned = Number(new URL(request.url).searchParams.get("version"));
  const version = pinned ? await versionOf(template.id, pinned) : await currentVersion(template.id);
  if (!version) throw new HttpError(404, "not_found", "no version of that number");

  return json(version.manifest, {
    headers: { "access-control-allow-origin": "*", "cache-control": "public, max-age=60" },
  });
});
```

- [ ] **Step 3: Add the two `GET` handlers to the existing routes**

`GET /api/v1/templates/{slug}` returns the record, its author, the current version with its source, and the list of version numbers. It refuses a draft to anyone but its author. `GET /api/v1/templates/{slug}/versions` returns the numbers with their notes and dates.

- [ ] **Step 4: Write the detail page**

`/templates/[slug]/page.tsx` is request-rendered and reads the database directly. Top to bottom: the photo, the name, the summary, the author with a link to `/u/<login>`, the like button from Task 18 once it exists, the tags. Then "What this creates", the `<Preview />` component from Task 15 over the stored manifest. Then "What you will be asked", a table of the inputs with their labels and help. Then the file, in `<SourceBlock />`: the stored YAML with a copy button, and under it the address of the JSON as a line of mono text, `GET https://cubeship.dev/api/v1/templates/<slug>/manifest`. No install command is shown, because none exists yet.

Then the versions: a list linking to `/templates/[slug]/versions/[number]`, newest marked current. Then the comments from Task 19.

`generateMetadata` sets the title, the description from the summary, the canonical URL, and the OG image to `/i/<key>` when there is a photo.

- [ ] **Step 5: Check and commit**

```bash
cd site && SITE_TEST_DATABASE_URL=postgres://site:site@localhost:5434/site pnpm test && pnpm run format && pnpm run typecheck
cd .. && git add site/src
git commit -m "Show a template, and serve the document an instance will read"
```

---

### Task 18: Likes

**Files:**
- Create: `site/src/lib/templates/social.ts`, `site/src/app/api/v1/templates/[slug]/like/route.ts`, `site/src/components/templates/like-button.tsx`
- Test: `site/src/lib/templates/social.db.test.ts`

**Interfaces:**
- Consumes: `db()`, `likes`, `templates`, `requireUser`.
- Produces: `setLike(user, slug, liked: boolean): Promise<{ likesCount: number; liked: boolean }>`, `hasLiked(userId, templateId): Promise<boolean>`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { createDraft, publishVersion } from "./publish";
import { setLike } from "./social";

describe.skipIf(!testDatabaseUrl)("setLike", () => {
  it("is idempotent in both directions and keeps the count", async () => {
    const handle = await withDatabase();
    const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
    const user = { id: row.id, login: row.login, name: null, avatarUrl: null, role: "user" as const };
    const { slug } = await createDraft(user, { name: "Demo", summary: "a demo template", tags: [] });
    await publishVersion(user, slug, {
      source: 'version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n    tag: "1"\n',
    });

    expect(await setLike(user, slug, true)).toEqual({ likesCount: 1, liked: true });
    expect(await setLike(user, slug, true)).toEqual({ likesCount: 1, liked: true });
    expect(await setLike(user, slug, false)).toEqual({ likesCount: 0, liked: false });
    expect(await setLike(user, slug, false)).toEqual({ likesCount: 0, liked: false });
  });
});
```

- [ ] **Step 2: Write `setLike`**

```ts
import { and, eq, sql } from "drizzle-orm";
import { db } from "@/db/client";
import { likes, templates } from "@/db/schema";
import type { SessionUser } from "@/lib/auth/session";
import { HttpError } from "@/lib/http";

// The count is denormalized so the catalog can sort on it without a join
// per row, and it is written in the same transaction as the row.
export async function setLike(user: SessionUser, slug: string, liked: boolean) {
  return db().transaction(async (tx) => {
    const [template] = await tx.select().from(templates).where(eq(templates.slug, slug)).limit(1);
    if (!template || template.status !== "published") {
      throw new HttpError(404, "not_found", "no template of that name");
    }

    const changed = liked
      ? await tx.insert(likes).values({ templateId: template.id, userId: user.id }).onConflictDoNothing().returning()
      : await tx
          .delete(likes)
          .where(and(eq(likes.templateId, template.id), eq(likes.userId, user.id)))
          .returning();

    if (changed.length === 0) return { likesCount: template.likesCount, liked };

    const [updated] = await tx
      .update(templates)
      .set({ likesCount: sql`${templates.likesCount} + ${liked ? 1 : -1}` })
      .where(eq(templates.id, template.id))
      .returning({ likesCount: templates.likesCount });

    return { likesCount: updated.likesCount, liked };
  });
}
```

- [ ] **Step 3: Write the route and the button**

`PUT` and `DELETE` on `/api/v1/templates/[slug]/like` call `requireUser` then `setLike`. The button is a client component: it takes the initial count and whether this viewer has liked it, updates optimistically, and on a 401 sends the viewer to `/api/auth/github?next=` the current path.

- [ ] **Step 4: Run, format, commit**

```bash
cd site && SITE_TEST_DATABASE_URL=postgres://site:site@localhost:5434/site pnpm test && pnpm run format
cd .. && git add site/src
git commit -m "Let a template be liked once per person"
```

---

### Task 19: Comments

**Files:**
- Modify: `site/src/lib/templates/social.ts`
- Create: `site/src/app/api/v1/templates/[slug]/comments/route.ts`, `site/src/app/api/v1/comments/[id]/route.ts`, `site/src/components/templates/comments.tsx`
- Test: `site/src/lib/templates/comments.db.test.ts`

**Interfaces:**
- Produces: `addComment(user, slug, input: { body: string; parentId?: number }): Promise<Comment>`, `listComments(templateId): Promise<Comment[]>`, `editComment(user, id, body): Promise<Comment>`, `deleteComment(user, id): Promise<void>`; `type Comment = { id: number; parentId: number | null; body: string | null; deleted: boolean; createdAt: Date; author: { login: string; avatarUrl: string | null } }`.

Rules to implement, each with a test: a comment is between 2 and 2000 characters; plain text, so it is stored as typed and rendered with whitespace preserved and no Markdown; at most 10 comments per person per 5 minutes, counted from the rows; a reply's parent must belong to the same template and must not itself be a reply, because there is exactly one level; an author may edit for 15 minutes and delete at any time; an admin may delete any; deleting sets `deletedAt` and blanks the body in the read, so the thread keeps its shape.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { addComment, deleteComment, listComments } from "./social";
import { createDraft, publishVersion } from "./publish";
import { templateBySlug } from "./queries";

async function seed() {
  const handle = await withDatabase();
  const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
  const user = { id: row.id, login: row.login, name: null, avatarUrl: null, role: "user" as const };
  const { slug } = await createDraft(user, { name: "Demo", summary: "a demo template", tags: [] });
  await publishVersion(user, slug, {
    source: 'version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n    tag: "1"\n',
  });
  return { user, slug };
}

describe.skipIf(!testDatabaseUrl)("comments", () => {
  it("keeps a reply under its parent", async () => {
    const { user, slug } = await seed();
    const parent = await addComment(user, slug, { body: "does this need a volume?" });
    await addComment(user, slug, { body: "no, it is stateless", parentId: parent.id });

    const template = await templateBySlug(slug);
    const thread = await listComments(template?.id ?? 0);

    expect(thread).toHaveLength(2);
    expect(thread[1].parentId).toBe(parent.id);
  });

  it("refuses a reply to a reply", async () => {
    const { user, slug } = await seed();
    const parent = await addComment(user, slug, { body: "first" });
    const reply = await addComment(user, slug, { body: "second", parentId: parent.id });

    await expect(addComment(user, slug, { body: "third", parentId: reply.id })).rejects.toMatchObject({ status: 422 });
  });

  it("refuses a body that is too short or too long", async () => {
    const { user, slug } = await seed();

    await expect(addComment(user, slug, { body: "x" })).rejects.toMatchObject({ status: 422 });
    await expect(addComment(user, slug, { body: "x".repeat(2001) })).rejects.toMatchObject({ status: 422 });
  });

  it("blanks a deleted comment but keeps the thread", async () => {
    const { user, slug } = await seed();
    const comment = await addComment(user, slug, { body: "never mind" });
    await deleteComment(user, comment.id);

    const template = await templateBySlug(slug);
    const [only] = await listComments(template?.id ?? 0);

    expect(only.deleted).toBe(true);
    expect(only.body).toBeNull();
  });

  it("rate-limits a flood", async () => {
    const { user, slug } = await seed();
    for (let i = 0; i < 10; i++) await addComment(user, slug, { body: `comment ${i}` });

    await expect(addComment(user, slug, { body: "one too many" })).rejects.toMatchObject({ status: 429 });
  });
});
```

- [ ] **Step 2: Implement them in `social.ts`, run until green**

- [ ] **Step 3: Write the routes and the UI**

`GET` and `POST` on `/api/v1/templates/[slug]/comments`; `PATCH` and `DELETE` on `/api/v1/comments/[id]`. The component renders the thread with `whitespace-pre-wrap`, never Markdown, shows a reply box under each top-level comment for signed-in viewers, and a sign-in link otherwise.

- [ ] **Step 4: Format, lint, commit**

```bash
cd site && SITE_TEST_DATABASE_URL=postgres://site:site@localhost:5434/site pnpm test && pnpm run format && pnpm run lint
cd .. && git add site/src
git commit -m "Talk about a template under it, in plain text"
```

---

### Task 20: Reports and the admin queue

**Files:**
- Modify: `site/src/lib/templates/social.ts`
- Create: `site/src/app/api/v1/reports/route.ts`, `site/src/app/api/v1/admin/reports/route.ts`, `site/src/app/api/v1/admin/reports/[id]/route.ts`, `site/src/app/api/v1/admin/users/[login]/route.ts`
- Create: `site/src/app/admin/reports/page.tsx`, `site/src/components/templates/report-button.tsx`
- Test: `site/src/lib/templates/reports.db.test.ts`

**Interfaces:**
- Produces: `report(user, input: { subjectType: "template" | "comment"; subjectId: number; reason: string; note?: string }): Promise<void>`, `openReports(): Promise<ReportRow[]>`, `resolveReport(admin, id, resolution: string): Promise<void>`, `setTemplateStatus(admin, slug, status): Promise<void>`, `blockUser(admin, login, blocked: boolean): Promise<void>`.

Rules with a test each: a reason is one of `spam`, `malware`, `abuse`, `other`; a person may report one subject once, enforced by a partial unique index on unresolved rows; only an admin reads the queue; blocking a user signs them out by deleting their sessions and hides their templates by setting each to `unlisted`.

- [ ] **Step 1: Write the failing test, covering the five rules above**
- [ ] **Step 2: Add the migration for the unique index**

```bash
cd site && DATABASE_URL=postgres://site:site@localhost:5434/site pnpm run db:generate
```

The index: `create unique index reports_one_per_person on reports (subject_type, subject_id, reporter_id) where resolved_at is null;`

- [ ] **Step 3: Implement, run until green**
- [ ] **Step 4: Write the admin page**

`/admin/reports/page.tsx` calls `requireAdmin()` and renders the open reports as rows: what was reported with a link to it, who reported it, why, and the actions. Each action is a form posting to the API and revalidating.

- [ ] **Step 5: Format, lint, commit**

```bash
cd site && SITE_TEST_DATABASE_URL=postgres://site:site@localhost:5434/site pnpm test && pnpm run format && pnpm run lint
cd .. && git add site/src site/drizzle
git commit -m "Give a bad template somewhere to be reported"
```

---

### Task 21: Wiring it into the site

**Files:**
- Modify: `site/src/lib/layout.shared.tsx`, `site/src/components/landing/footer.tsx`, `site/src/app/(home)/page.tsx`, `site/src/app/sitemap.ts`
- Create: `site/src/components/landing/templates-strip.tsx`, `site/src/app/u/[login]/page.tsx`, `site/src/app/me/templates/page.tsx`, `site/src/app/og/templates/[slug]/route.tsx`
- Modify: `docs/design/site.md`, `docs/design/templates.md`, `README.md`

**Interfaces:**
- Consumes: `listTemplates`, `findByLogin`.
- Produces: nothing code depends on.

- [ ] **Step 1: Add the navigation**

A `Templates` link in `layout.shared.tsx` beside Docs and Changelog, and one in the footer's Product column in `footer.tsx`. The footer takes one line, because it is a list of groups.

- [ ] **Step 2: Add the landing strip**

`<TemplatesStrip />` reads the four most-liked published templates and renders them as cards under a mono label, with a link to the catalog. It is a server component and the landing page becomes request-rendered, which is the price of showing live rows there; if that is unwanted later, the strip moves behind `revalidate`.

- [ ] **Step 3: Add the two profile pages**

`/u/[login]` lists that author's published templates, with `notFound()` for an unknown login. `/me/templates` calls `currentUser()`, redirects to sign-in when there is none, and lists that person's drafts, published and unlisted templates with links to edit each.

- [ ] **Step 4: Add the OG image and the sitemap entries**

`/og/templates/[slug]/route.tsx` follows the existing `/og/docs/[...slug]` route: the template's name, its summary and the wordmark. In `sitemap.ts`, add the published templates and `/templates`; the file becomes request-rendered, so add `export const dynamic = "force-dynamic"` and make sure `next build` never queries by keeping the query inside the handler.

- [ ] **Step 5: Update the three documents**

In `docs/design/site.md`, the opening says the site talks to nothing and is compiled in. Correct it: the landing page and the docs are still static, and `/templates` and its API read Postgres and a bucket. Point at `templates.md` for the rest.

In `docs/design/templates.md`, correct whatever the build proved wrong. Two are already known and were fixed in Task 10, so check they read correctly: migrations run in `instrumentation.ts`, and there is no `SESSION_SECRET`. Correcting the design document is part of this task, not an afterthought.

In `README.md`, add templates to the feature list, one sentence, pointing at `https://cubeship.dev/templates`.

- [ ] **Step 6: The full gate, then commit**

```bash
cd site && pnpm run format && pnpm run lint && pnpm run typecheck && SITE_TEST_DATABASE_URL=postgres://site:site@localhost:5434/site pnpm test && pnpm run build
cd .. && make check
git add site/src docs README.md
git commit -m "Put templates in the navigation, the sitemap and the README"
```

- [ ] **Step 7: Confirm the image builds and starts with a database**

```bash
make site-image
docker run --rm -p 3010:3000 \
  -e DATABASE_URL=postgres://site:site@host.docker.internal:5434/site \
  cubeship-site:$(git describe --tags --always)
```

Expected: the log shows the migrations applying once, and `http://localhost:3010/templates` answers.

---

## What is deliberately not here

- **The daemon's half.** No Go changes: no `cubeship template apply`, no manifest applier, no dashboard browser. It gets its own design and its own plan, written against `/api/v1/templates/{slug}/manifest`.
- **An install count.** Nothing can write one until an instance reports an apply.
- **Account deletion and author transfer.** Sign-out is all there is.
- **API reference generated from `openapi.json`.** Still the separate, already-deferred piece of work.
