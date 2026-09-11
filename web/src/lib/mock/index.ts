// The dashboard with no daemon behind it.
//
// Every screen here reaches the API through one function — `request` in
// lib/api.ts — so standing the whole interface up on invented data is a
// switch in that one place rather than a second way of building the
// app. What you get is the real routing, the real components and the
// real loading states, against a fixed instance that needs no Docker,
// no Postgres and no network.
//
// **It is not a test double and does not pretend to be the daemon.** It
// answers what a screen asks for so the screen can be looked at. Where
// the two disagree the daemon is right, and a mock that drifted is a
// preview that lies — which is why an unknown path is a loud refusal
// below rather than an empty array.
//
// Turned on by NEXT_PUBLIC_CUBESHIP_MOCK=1, which `make web-preview`
// sets. next.config.ts aliases this module to a stub in every other
// build, which is what keeps the fixtures out of the image people run.

import type {
  Bucket,
  DNSProviderKind,
  DNSRecord,
  DNSZone,
  ObjectListing,
  RegistryImage,
  RegistryRepository,
  RegistryStatus,
  RegistryUsage,
} from "@/lib/api";
import { db, type Row, series } from "./db";

// Latency, so the loading states are things you can see rather than
// things that flash. A preview where everything is instant is one where
// the skeletons are never reviewed.
const LATENCY_MS = 180;

type Handler = (p: string[], body?: unknown) => unknown;

// A route is a method, a pattern and a handler. `:x` captures a
// segment, `*` captures the rest — matched in order, first wins.
const routes: [string, string, Handler][] = [
  ["GET", "/setup", () => db.setup],
  ["GET", "/users/me", () => db.me],
  ["PATCH", "/users/me", (_p, body) => Object.assign(db.me, body as Row)],
  ["GET", "/users", () => ({ users: db.users })],
  [
    "POST",
    "/users",
    (_p, body) => {
      const b = body as Row;
      const created = {
        username: b.username as string,
        role: (b.role as "admin" | "member") ?? "member",
        avatar: "mono",
        created_at: new Date().toISOString(),
      };
      db.users.push(created);
      return { ...created, password: "correct-horse-battery-staple" };
    },
  ],
  [
    "PATCH",
    "/users/:username",
    (p, body) => {
      const b = body as Row;
      const u = db.users.find((x) => x.username === p[0]);
      if (!u) return null;
      if (typeof b.role === "string") u.role = b.role as "admin" | "member";
      if (typeof b.blocked === "boolean") {
        u.blocked_at = b.blocked ? new Date().toISOString() : undefined;
      }
      return u;
    },
  ],
  [
    "POST",
    "/users/:username/password",
    (p) => ({ username: p[0], password: "correct-horse-battery-staple" }),
  ],
  [
    "DELETE",
    "/users/:username",
    (p) => {
      db.users = db.users.filter((u) => u.username !== p[0]);
      return null;
    },
  ],
  ["DELETE", "/users/:username/credentials", () => ({ api_keys: 2, sessions: 1 })],
  ["GET", "/users/me/api-keys", () => db.apiKeys],
  ["GET", "/settings", () => db.settings],
  ["PATCH", "/settings", (_p, body) => Object.assign(db.settings, body as Row)],
  ["GET", "/updates", () => db.updates],
  ["GET", "/release", () => ({ version: db.updates.current, notes: "", seen: true })],

  // --- projects, environments, apps ---
  ["GET", "/projects", () => db.projects],
  // The picture is the one thing the preview cannot invent: there are
  // no bytes to serve and nothing to upload them to. Accepting the
  // write and remembering the flag is what the screen is actually
  // about — whether a project wears one, and what it looks like when it
  // does not.
  [
    "PUT",
    "/projects/:slug/image",
    (p) => {
      const project = db.projects.find((x) => x.slug === p[0]);
      if (project) project.has_image = true;
      return null;
    },
  ],
  [
    "DELETE",
    "/projects/:slug/image",
    (p) => {
      const project = db.projects.find((x) => x.slug === p[0]);
      if (project) project.has_image = false;
      return null;
    },
  ],
  [
    "POST",
    "/projects",
    (_p, body) => {
      const slug = (body as Row).slug as string;
      const created = { slug, environments: ["production"] };
      db.projects.push({ slug });
      db.environments[slug] = [{ slug: "production" }];
      return created;
    },
  ],
  [
    "DELETE",
    "/projects/:project",
    (p) => {
      db.projects = db.projects.filter((x) => x.slug !== p[0]);
      db.apps = db.apps.filter((a) => a.project !== p[0]);
      return {};
    },
  ],
  ["GET", "/projects/:project/environments", (p) => db.environments[p[0]] ?? []],
  [
    "POST",
    "/projects/:project/environments",
    (p, body) => {
      const slug = (body as Row).slug as string;
      db.environments[p[0]] ??= [];
      db.environments[p[0]].push({ slug });
      return { slug };
    },
  ],
  [
    "DELETE",
    "/projects/:project/environments/:env",
    (p) => {
      db.environments[p[0]] = (db.environments[p[0]] ?? []).filter((e) => e.slug !== p[1]);
      return {};
    },
  ],

  ["GET", "/projects/:project/env", (p) => ({ vars: db.env.project[p[0]] ?? {} })],
  [
    "GET",
    "/projects/:project/environments/:env/env",
    (p) => ({
      vars: db.env.environment[`${p[0]}/${p[1]}`] ?? {},
      effective: resolved(db.env.project[p[0]], db.env.environment[`${p[0]}/${p[1]}`]),
    }),
  ],

  ["GET", "/apps", () => db.apps],
  [
    "POST",
    "/apps",
    (_p, body) => {
      const b = body as Row;
      const app: Row = {
        reference: `${b.project}/${b.environment}/${b.name}`,
        name: b.name,
        project: b.project,
        environment: b.environment,
        source: "registry",
        image: `registry.cubeship.example.com/${b.project}/${b.environment}/${b.name}`,
        autodeploy: true,
        status: "down",
        has_container: false,
        domains: [],
        nodes: ["control-plane"],
        replicas: [],
        scale: 1,
        spread: false,
        split: false,
        address: "203.0.113.42",
        limits: { cpu: 0, memory: 0 },
        autoscale: { min: 0, max: 0, cpu: 0 },
      };
      db.apps.push(app);
      return app;
    },
  ],
  ["GET", "/apps/:a/:b/:c", (p) => appOr404(p.join("/"))],
  ["PATCH", "/apps/:a/:b/:c", (p, body) => Object.assign(appOr404(p.join("/")), body as Row)],
  [
    "DELETE",
    "/apps/:a/:b/:c",
    (p) => {
      db.apps = db.apps.filter((a) => a.reference !== p.join("/"));
      return {};
    },
  ],
  ["GET", "/apps/:a/:b/:c/env", (p) => appEnv(p.join("/"))],
  ["GET", "/apps/:a/:b/:c/deployments", (p) => db.deployments[p.join("/")] ?? []],
  [
    "POST",
    "/apps/:a/:b/:c/deploy",
    (p) => {
      const ref = p.join("/");
      const row = {
        id: Math.floor(Math.random() * 900) + 100,
        status: "succeeded",
        tag: "latest",
        live: true,
        deletable: true,
        has_logs: true,
        created_at: new Date().toISOString(),
        finished_at: new Date().toISOString(),
      };
      db.deployments[ref] ??= [];
      db.deployments[ref].unshift(row);
      const app = db.apps.find((a) => a.reference === ref);
      if (app) {
        app.status = "running";
        app.has_container = true;
      }
      return row;
    },
  ],
  ["GET", "/apps/:a/:b/:c/logs", () => ({ logs: sampleLog })],
  ["GET", "/apps/:a/:b/:c/metrics", () => containerSeries(24, 512 * 1024 * 1024)],
  ["GET", "/apps/:a/:b/:c/domains", (p) => appOr404(p.join("/")).domains],

  // --- the instance itself ---
  ["GET", "/instance/metrics", () => instanceSeries()],
  ["GET", "/instance/containers", () => containers()],

  // --- databases ---
  ["GET", "/datastores/engines", () => engines],
  ["GET", "/datastores", () => db.datastores],
  ["GET", "/datastores/:name", (p) => row(db.datastores, "name", p[0])],
  ["GET", "/datastores/:name/credentials", (p) => credentialsFor(p[0])],
  ["GET", "/datastores/:name/attachments", (p) => row(db.datastores, "name", p[0]).attachments],
  ["GET", "/datastores/:name/logs", () => ({ logs: sampleLog })],
  ["GET", "/datastores/:name/metrics", () => containerSeries(24, 2 * 1024 * 1024 * 1024)],
  ["GET", "/datastores/:name/backups", (p) => db.backups.filter((b) => b.database === p[0])],
  [
    "POST",
    "/datastores/:name/backups",
    (p) => {
      const b: Row = {
        id: Math.floor(Math.random() * 900) + 100,
        database: p[0],
        database_exists: true,
        engine: "postgres",
        version: "18",
        store: "offsite",
        bucket: "dumps",
        key: `cubeship/${p[0]}/${new Date().toISOString()}.dump`,
        off_machine: true,
        size_bytes: 0,
        status: "taking",
        scheduled: false,
        started_at: new Date().toISOString(),
      };
      db.backups.unshift(b);
      // Finishes shortly, so the in-flight badge is a state you can
      // actually watch rather than one you have to imagine.
      setTimeout(() => {
        b.status = "succeeded";
        b.size_bytes = 48_000_000;
        b.finished_at = new Date().toISOString();
      }, 4000);
      return b;
    },
  ],
  ["GET", "/datastores/:name/backups/schedule", (p) => db.backupSchedules[p[0]] ?? notFound()],
  [
    "PUT",
    "/datastores/:name/backups/schedule",
    (p, body) => {
      db.backupSchedules[p[0]] = { ...(body as Row), last_run_at: undefined };
      return db.backupSchedules[p[0]];
    },
  ],
  [
    "DELETE",
    "/datastores/:name/backups/schedule",
    (p) => {
      delete db.backupSchedules[p[0]];
      return {};
    },
  ],

  // --- backups, instance-wide ---
  ["GET", "/backups", () => db.backups],
  // **Derived rather than written out.** A hand-written coverage
  // fixture is a second answer to what the backups say, and it goes
  // stale the first time somebody edits one of them — in a preview,
  // silently, which is the one place a wrong answer looks finished.
  ["GET", "/backups/coverage", () => coverage()],
  ["GET", "/backups/orphans", () => db.backups.filter((b) => !b.database_exists)],

  // --- the instance backing itself up ---
  ["GET", "/instance/backups", () => db.instanceBackups],
  [
    "POST",
    "/instance/backups",
    () => {
      const row = {
        id: 900 + db.instanceBackups.length,
        kind: "instance",
        database: "cubeship",
        database_exists: true,
        engine: "postgres",
        version: "16",
        store: "offsite",
        bucket: "dumps",
        key: `cubeship/cubeship/${new Date().toISOString()}.dump`,
        off_machine: true,
        size_bytes: 2_140_882,
        status: "succeeded",
        scheduled: false,
        started_at: new Date().toISOString(),
        finished_at: new Date().toISOString(),
      };
      db.instanceBackups.unshift(row);
      return row;
    },
  ],
  ["GET", "/instance/backups/schedule", () => db.instanceSchedule ?? notFound()],
  [
    "PUT",
    "/instance/backups/schedule",
    (_p, body) => {
      db.instanceSchedule = { ...(body as Row), last_run_at: undefined };
      return db.instanceSchedule;
    },
  ],
  [
    "DELETE",
    "/instance/backups/schedule",
    () => {
      db.instanceSchedule = null;
      return null;
    },
  ],
  [
    "DELETE",
    "/backups/:id",
    (p) => {
      db.backups = db.backups.filter((b) => String(b.id) !== p[0]);
      return {};
    },
  ],
  ["POST", "/backups/:id/restore", () => ({})],

  // --- object storage ---
  ["GET", "/objectstores/providers", () => providers],
  ["GET", "/objectstores", () => db.objectStores],
  ["GET", "/objectstores/:name", (p) => row(db.objectStores, "name", p[0])],
  ["GET", "/objectstores/:name/buckets", () => buckets],
  ["GET", "/objectstores/:name/buckets/:bucket/objects", () => listing],
  ["GET", "/objectstores/:name/attachments", () => []],
  [
    "GET",
    "/objectstores/:name/credentials",
    () => ({ access_key: "AKIAPREVIEW", secret_key: "s3cr3t-preview" }),
  ],
  ["GET", "/objectstores/:name/logs", () => ({ logs: sampleLog })],
  ["GET", "/objectstores/:name/metrics", () => containerSeries(24, 1024 * 1024 * 1024)],

  // --- platform ---
  ["GET", "/registries", () => db.registries],
  // `/registry` — singular, no id — is the one this instance runs. The
  // dashboard addresses it there rather than under an id, because it
  // has no credential row to have one.
  ["GET", "/registry/repositories", () => repositories],
  ["GET", "/registry/images", () => images],
  ["GET", "/registry/usage", () => usage],
  ["GET", "/registries/tags", () => ({ tags: ["v1.4.0", "v1.3.2", "latest"] })],
  ["GET", "/registries/:id", (p) => row(db.registries, "id", p[0])],
  ["GET", "/registries/:id/repositories", () => repositories],
  ["GET", "/registries/:id/images", () => images],
  ["GET", "/registries/:id/usage", () => usage],
  ["GET", "/registries/:id/status", () => ({ state: "available" }) as RegistryStatus],
  ["GET", "/credentials", () => db.credentials],
  // `/dns/providers` before `/dns/:id`: the literal wins, which is the
  // same order Go's mux resolves them in on the daemon.
  ["GET", "/dns/providers", () => dnsKinds],
  ["GET", "/dns", () => db.dnsProviders],
  ["GET", "/dns/:id/status", () => ({ state: "available" })],
  ["GET", "/dns/:id/zones", () => zones],
  ["GET", "/dns/:id/records", () => records],
  ["GET", "/certificates", () => db.certificates],
  ["GET", "/firewall", () => db.firewall],
  [
    "POST",
    "/firewall/:action",
    (p) => {
      if (p[0] === "enable") db.firewall.enabled = true;
      if (p[0] === "disable") db.firewall.enabled = false;
      return db.firewall;
    },
  ],
  ["GET", "/nodes", () => db.nodes],
  ["GET", "/nodes/mesh", () => db.mesh],
  ["GET", "/github", () => db.github],
  ["GET", "/github/repositories", () => githubRepos],
];

// --- the switchboard ---

export async function handle(method: string, path: string, body?: unknown): Promise<unknown> {
  await new Promise((r) => setTimeout(r, LATENCY_MS));

  const [clean] = path.split("?");
  const parts = clean.split("/").filter(Boolean);

  for (const [m, pattern, fn] of routes) {
    if (m !== method) continue;
    const captured = match(pattern, parts);
    // **Copied, because a network gives you a copy.** The handlers
    // return rows out of `db` directly, so without this a caller holds
    // the very object the next write mutates — and React, handed the
    // same array identity it already has, skips the render. A row
    // edited in place then changed nothing on screen until something
    // else happened to re-render, which is a bug that exists only in
    // the preview and looks exactly like one in the product.
    if (captured) return structuredClone(fn(captured, body));
  }

  // **Loud rather than empty.** A mock that answered `[]` for a path it
  // does not know would render a screen that looks finished and is
  // wrong, and the person reviewing it would have no way to tell the
  // difference. This lands in the page's own error alert with the path
  // to add.
  throw new MockGap(`${method} ${clean}`);
}

export class MockGap extends Error {
  // Deliberately not a status the product uses. 501 was the first
  // choice and collided head-on: a registry answers 501 when it will
  // not list its catalogue, so a missing mock rendered as "this
  // registry does not list what it holds" — a screen explaining a
  // decision nobody made.
  status = 599;
  constructor(route: string) {
    super(`preview has no mock for ${route} — add it in src/lib/mock/index.ts`);
  }
}

// coverage mirrors what the daemon builds: one row per database, from
// the databases — so a database nobody has ever backed up is in it.
function coverage(): Row[] {
  return db.datastores.map((d) => {
    const name = d.name as string;
    const mine = db.backups.filter((b) => b.database === name && b.database_exists);
    const last = mine[0];
    const lastGood = mine.find((b) => b.status === "succeeded");
    return {
      database: name,
      engine: d.engine,
      version: d.version,
      can_back_up: d.can_back_up,
      schedule: db.backupSchedules[name],
      protected: Boolean(lastGood?.off_machine),
      failing: last?.status === "failed",
      last_good: lastGood,
      last,
      count: mine.length,
    };
  });
}

function match(pattern: string, parts: string[]): string[] | null {
  const want = pattern.split("/").filter(Boolean);
  if (want.length !== parts.length) return null;
  const captured: string[] = [];
  for (let i = 0; i < want.length; i++) {
    if (want[i].startsWith(":")) {
      captured.push(decodeURIComponent(parts[i]));
      continue;
    }
    if (want[i] !== parts[i]) return null;
  }
  return captured;
}

// --- small helpers the handlers lean on ---

function row(rows: Row[], key: string, value: string): Row {
  const found = rows.find((r) => String(r[key]) === value);
  if (!found) return notFound();
  return found;
}

function notFound(): never {
  const e = new Error("not found") as Error & { status: number };
  e.status = 404;
  throw e;
}

function appOr404(reference: string): Row {
  return row(db.apps, "reference", reference);
}

function resolved(...layers: (Record<string, string> | undefined)[]) {
  const out: Record<string, { value: string; source: string }> = {};
  const names = ["project", "environment", "app"];
  layers.forEach((layer, i) => {
    for (const [k, v] of Object.entries(layer ?? {})) out[k] = { value: v, source: names[i] };
  });
  return out;
}

function appEnv(reference: string) {
  const app = appOr404(reference);
  return {
    vars: db.env.app[reference] ?? {},
    effective: resolved(
      db.env.project[app.project as string],
      db.env.environment[`${app.project}/${app.environment}`],
      db.env.app[reference],
    ),
  };
}

function containerSeries(points: number, limit: number) {
  const cpu = series(points, 35, 25);
  const mem = series(points, limit * 0.45, limit * 0.12);
  return {
    window: "1h",
    collecting: true,
    memory_limit_bytes: limit,
    samples: cpu.map((s, i) => ({
      at: s.at,
      cpu_percent: s.value,
      memory_bytes: mem[i].value,
      memory_limit_bytes: limit,
    })),
  };
}

function instanceSeries() {
  const total = 8 * 1024 * 1024 * 1024;
  const disk = 160 * 1024 * 1024 * 1024;
  const cpu = series(48, 28, 18);
  const mem = series(48, total * 0.52, total * 0.08);
  const rx = series(48, 240_000, 180_000);
  const tx = series(48, 120_000, 90_000);
  return {
    window: "1h",
    cores: 4,
    memory_total_bytes: total,
    disk_total_bytes: disk,
    disk_path: "/var/lib/cubeship",
    interfaces: ["eth0"],
    samples: cpu.map((s, i) => ({
      at: s.at,
      cpu_percent: s.value,
      memory_bytes: mem[i].value,
      memory_total_bytes: total,
      disk_bytes: disk * 0.37,
      disk_total_bytes: disk,
      rx_bytes_per_sec: rx[i].value,
      tx_bytes_per_sec: tx[i].value,
    })),
  };
}

function containers() {
  const at = new Date().toISOString();
  return [
    {
      kind: "datastore",
      name: "pg",
      at,
      cpu_percent: 62,
      memory_bytes: 910_000_000,
      memory_limit_bytes: 2147483648,
    },
    {
      kind: "app",
      name: "web/production/api",
      at,
      cpu_percent: 41,
      memory_bytes: 180_000_000,
      memory_limit_bytes: 0,
    },
    {
      kind: "app",
      name: "internal/production/docs",
      at,
      cpu_percent: 3,
      memory_bytes: 24_000_000,
      memory_limit_bytes: 0,
    },
    {
      kind: "objectstore",
      name: "uploads",
      at,
      cpu_percent: 1,
      memory_bytes: 61_000_000,
      memory_limit_bytes: 0,
    },
  ];
}

function credentialsFor(name: string) {
  const d = row(db.datastores, "name", name);
  return {
    username: d.username,
    password: "preview-password",
    database: d.database,
    host: d.host,
    port: d.port,
    uri: `postgres://${d.username}:preview-password@${d.host}:${d.port}/${d.database}`,
  };
}

// --- static answers ---

const engines = [
  { engine: "postgres", label: "PostgreSQL", versions: ["18", "17", "16", "15"] },
  { engine: "mysql", label: "MySQL", versions: ["8.4", "8.0"] },
  { engine: "mariadb", label: "MariaDB", versions: ["11", "10.11"] },
  { engine: "redis", label: "Redis", versions: ["7"] },
  { engine: "mongodb", label: "MongoDB", versions: ["7"] },
];

const providers = {
  providers: [
    { provider: "aws", label: "Amazon S3", asks: "region", scopes_by_bucket: false },
    { provider: "cloudflare", label: "Cloudflare R2", asks: "account", scopes_by_bucket: true },
    {
      provider: "digitalocean",
      label: "DigitalOcean Spaces",
      asks: "region",
      scopes_by_bucket: true,
    },
    { provider: "generic", label: "S3-compatible", asks: "endpoint", scopes_by_bucket: false },
  ],
  versions: ["RELEASE.2025-04-22T22-12-26Z"],
};

const buckets: Bucket[] = [
  { name: "dumps", created_at: new Date(Date.now() - 86_400_000 * 40).toISOString() },
  { name: "uploads", created_at: new Date(Date.now() - 86_400_000 * 12).toISOString() },
];

// A folder is `{prefix, name}` and an object carries `name` and
// `modified_at` beside its key — the shapes in ObjectListing. Written
// loosely the first time (bare strings for folders, `last_modified` for
// the date) it rendered a table whose rows had no React key and whose
// dates were blank: the preview being wrong about the daemon, rather
// than the dashboard being wrong about anything.
const listing: ObjectListing = {
  prefix: "",
  folders: [{ prefix: "2026/", name: "2026" }],
  objects: [
    {
      key: "readme.txt",
      name: "readme.txt",
      size: 812,
      modified_at: new Date(Date.now() - 86_400_000).toISOString(),
    },
    {
      key: "logo.png",
      name: "logo.png",
      size: 48_120,
      modified_at: new Date(Date.now() - 86_400_000 * 3).toISOString(),
    },
  ],
};

const repositories: RegistryRepository[] = [
  { name: "web/production/api" },
  { name: "web/staging/api" },
];

const images: RegistryImage[] = [
  {
    tag: "latest",
    digest: "sha256:9f2a…c104",
    size: 74_210_880,
    pushed_at: new Date(Date.now() - 95 * 60_000).toISOString(),
  },
  {
    tag: "v1.4.0",
    digest: "sha256:9f2a…c104",
    size: 74_210_880,
    pushed_at: new Date(Date.now() - 95 * 60_000).toISOString(),
  },
  {
    tag: "v1.3.2",
    digest: "sha256:41bd…7e90",
    size: 73_884_112,
    pushed_at: new Date(Date.now() - 86_400_000 * 6).toISOString(),
  },
];

const usage: RegistryUsage = {
  total_bytes: 402_113_440,
  counts_shared_layers: true,
  repositories: [
    { name: "web/production/api", bytes: 301_998_220, images: 3 },
    { name: "web/staging/api", bytes: 100_115_220, images: 1 },
  ],
};

const dnsKinds: DNSProviderKind[] = [
  {
    provider: "cloudflare",
    name: "Cloudflare",
    password_label: "API token",
    hint: "A token with Zone:Read and DNS:Edit on the zones this instance should write.",
  },
  {
    provider: "route53",
    name: "Route 53",
    username_label: "Access key ID",
    password_label: "Secret access key",
    hint: "An IAM key with route53:ChangeResourceRecordSets on the hosted zones.",
  },
];

const zones: DNSZone[] = [
  { id: "z1", name: "example.com" },
  { id: "z2", name: "example.net" },
];

// A record is a list of values at both providers: two A records for one
// name are one record here.
const records: DNSRecord[] = [
  {
    id: "r1",
    type: "A",
    name: "api.example.com",
    values: ["203.0.113.42"],
    ttl: 300,
    proxied: false,
  },
  {
    id: "r2",
    type: "A",
    name: "docs.example.com",
    values: ["203.0.113.42"],
    ttl: 300,
    proxied: true,
  },
  { id: "r3", type: "TXT", name: "example.com", values: ["v=spf1 -all"], ttl: 3600 },
];

const githubRepos = [
  { full_name: "acme/worker", private: true, default_branch: "main" },
  { full_name: "acme/site", private: false, default_branch: "main" },
];

// A log with colour in it, so the ANSI rendering is visible in the
// preview rather than something you have to deploy to see.
const sampleLog = [
  "[2m2026-09-11T05:12:01.001Z[0m [32m INFO[0m api listening [3maddress[0m[2m=[0m0.0.0.0:3000",
  "[2m2026-09-11T05:12:01.004Z[0m [32m INFO[0m connected to postgres",
  "[2m2026-09-11T05:12:44.820Z[0m [33m WARN[0m slow query took 1.2s",
  "[2m2026-09-11T05:13:02.115Z[0m [31mERROR[0m upstream timed out, retrying",
  "[2m2026-09-11T05:13:02.900Z[0m [32m INFO[0m recovered",
].join("\n");
