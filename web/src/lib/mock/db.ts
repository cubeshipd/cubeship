// The instance the preview pretends to be.
//
// One VPS with a couple of projects on it, a Postgres, a linked bucket
// and a second machine — enough that every screen has something on it
// and the interesting states are represented: an app that is down, a
// deploy that failed, a certificate that is pending, a backup on the
// local disk.
//
// **It is mutable on purpose.** Creating a project in the preview adds
// a row here, so the screens behave rather than just render. Nothing is
// persisted; a reload is a fresh instance.
//
// **The answers that have a type wear it.** A fixture is a claim about
// what the daemon sends, and a wrong claim is a preview that lies —
// which it did three times before these annotations: a certificate
// report keyed on `domain` instead of `host` rendered a table of blank
// names, a firewall missing `available` rendered the sentence for a
// daemon that cannot read one, and a bucket listing with bare strings
// for folders rendered rows React could not key. All three compile-time
// errors now. The rows that handlers mutate stay loose on purpose:
// `Object.assign` against a strict type is a fight for no gain in a
// file whose whole job is to be edited.

import type {
  ApiKey,
  CertificateReport,
  ClusterServer,
  DNSProvider,
  Firewall,
  InstanceUser,
  Me,
  RegistryCredential,
  Settings,
} from "@/lib/api";

const now = Date.now();
const ago = (minutes: number) => new Date(now - minutes * 60_000).toISOString();

export type Row = Record<string, unknown>;

export const db = {
  setup: { needed: false, token_required: false },

  me: {
    username: "lucas",
    role: "admin" as const,
    has_password: true,
    themes: ["cyan", "mono", "hacker", "red", "orange", "pink", "purple", "blue"],
    display_name: "Lucas",
    email: "lucas@example.com",
    avatar: "cyan",
    avatars: ["blue", "cyan", "hacker", "mono", "orange", "pink", "purple", "red"],
  } as Me,

  users: [
    { username: "lucas", role: "admin", avatar: "cyan", created_at: ago(60 * 24 * 90) },
    { username: "ci", role: "member", avatar: "hacker", created_at: ago(60 * 24 * 12) },
  ] as InstanceUser[],

  apiKeys: [
    {
      id: 1,
      name: "laptop",
      created_at: ago(60 * 24 * 30),
      last_used_at: ago(140),
      current_key: true,
    },
    { id: 2, name: "ci", created_at: ago(60 * 24 * 8), current_key: false },
  ] as ApiKey[],

  settings: {
    domain: "cubeship.example.com",
    acme_email: "ops@example.com",
    registry_host: "registry.cubeship.example.com",
    public_ip: "203.0.113.42",
    public_ip_configured: false,
    tls_enabled: true,
    wildcard_domain: false,
    github_connected: true,
    github_app_slug: "cubeship-example",
    github_oauth_ready: true,
    auto_update_at: "03:00",
    auto_update_timezone: "America/Sao_Paulo",
  } as Settings,

  projects: [{ slug: "web" }, { slug: "internal" }] as Row[],

  environments: {
    web: [{ slug: "production" }, { slug: "staging" }],
    internal: [{ slug: "production" }],
  } as Record<string, Row[]>,

  apps: [
    {
      reference: "web/production/api",
      name: "api",
      project: "web",
      environment: "production",
      source: "registry",
      image: "registry.cubeship.example.com/web/production/api",
      autodeploy: true,
      status: "running",
      has_container: true,
      domains: [{ id: 1, host: "api.example.com", port: 3000 }],
      suggested_host: "api.production.web.cubeship.example.com",
      nodes: ["control-plane"],
      replicas: [
        {
          node: "control-plane",
          ordinal: 0,
          status: "running",
          deployment_id: 41,
          container_name: "cubeship-web-production-api-41-0",
        },
      ],
      scale: 1,
      spread: false,
      split: false,
      address: "203.0.113.42",
      limits: { cpu: 0, memory: 0 },
      autoscale: { min: 0, max: 0, cpu: 0 },
      health_path: "/healthz",
    },
    {
      reference: "web/production/worker",
      name: "worker",
      project: "web",
      environment: "production",
      source: "railpack",
      repo: "https://github.com/acme/worker",
      ref: "main",
      status: "down",
      has_container: false,
      domains: [],
      nodes: ["control-plane"],
      replicas: [],
      scale: 1,
      spread: false,
      split: false,
      address: "203.0.113.42",
      limits: { cpu: 1, memory: 536870912 },
      autoscale: { min: 0, max: 0, cpu: 0 },
    },
    {
      reference: "web/staging/api",
      name: "api",
      project: "web",
      environment: "staging",
      source: "registry",
      image: "registry.cubeship.example.com/web/staging/api",
      tag: "v1.4.0",
      autodeploy: false,
      status: "running",
      has_container: true,
      domains: [{ id: 2, host: "api.staging.example.com", port: 3000 }],
      nodes: ["control-plane"],
      replicas: [
        {
          node: "control-plane",
          ordinal: 0,
          status: "running",
          deployment_id: 39,
          container_name: "cubeship-web-staging-api-39-0",
        },
      ],
      scale: 1,
      spread: false,
      split: false,
      address: "203.0.113.42",
      limits: { cpu: 0, memory: 0 },
      autoscale: { min: 0, max: 0, cpu: 0 },
    },
    {
      reference: "internal/production/docs",
      name: "docs",
      project: "internal",
      environment: "production",
      source: "external",
      image: "docker.io/library/nginx",
      status: "running",
      has_container: true,
      domains: [{ id: 3, host: "docs.example.com", port: 80 }],
      nodes: ["worker-1"],
      replicas: [
        {
          node: "worker-1",
          ordinal: 0,
          status: "running",
          deployment_id: 38,
          container_name: "cubeship-internal-production-docs-38-0",
        },
      ],
      scale: 1,
      spread: false,
      split: false,
      address: "203.0.113.42",
      limits: { cpu: 0, memory: 0 },
      autoscale: { min: 0, max: 0, cpu: 0 },
    },
  ] as Row[],

  deployments: {
    "web/production/api": [
      {
        id: 41,
        status: "succeeded",
        image_ref: "registry.cubeship.example.com/web/production/api:latest",
        tag: "latest",
        live: true,
        deletable: true,
        has_logs: true,
        created_at: ago(95),
        finished_at: ago(94),
      },
      {
        id: 40,
        status: "failed",
        tag: "latest",
        error: "container exited with status 1 before it became healthy",
        live: false,
        deletable: true,
        has_logs: true,
        created_at: ago(220),
        finished_at: ago(219),
      },
    ],
    "web/production/worker": [
      {
        id: 37,
        status: "failed",
        tag: "main",
        error: "railpack: no start command found in this repository",
        live: false,
        deletable: true,
        has_logs: true,
        created_at: ago(1400),
        finished_at: ago(1398),
      },
    ],
  } as Record<string, Row[]>,

  env: {
    project: { web: { NODE_ENV: "production" }, internal: {} },
    environment: { "web/production": { LOG_LEVEL: "info" }, "web/staging": { LOG_LEVEL: "debug" } },
    app: { "web/production/api": { PORT: "3000" } },
  } as Record<string, Record<string, Record<string, string>>>,

  datastores: [
    {
      name: "pg",
      engine: "postgres",
      var_stem: "DATABASE",
      version: "18",
      status: "running",
      has_container: true,
      username: "cubeship",
      database: "cubeship",
      host: "cubeship-db-pg",
      port: 5432,
      exposed_port: 0,
      limits: { cpu: 0, memory: 2147483648 },
      can_back_up: true,
      backup_consistency: "a consistent snapshot, taken while the database keeps serving",
      attachments: [{ app: "web/production/api", prefix: "" }],
    },
    {
      name: "orders",
      engine: "postgres",
      var_stem: "DATABASE",
      version: "17",
      status: "running",
      has_container: true,
      username: "cubeship",
      database: "cubeship",
      host: "cubeship-db-orders",
      port: 5432,
      exposed_port: 0,
      limits: { cpu: 0, memory: 0 },
      can_back_up: true,
      backup_consistency: "a consistent snapshot, taken while the database keeps serving",
      attachments: [],
    },
    {
      name: "events",
      engine: "mysql",
      var_stem: "DATABASE",
      version: "8.4",
      status: "running",
      has_container: true,
      username: "cubeship",
      database: "cubeship",
      host: "cubeship-db-events",
      port: 3306,
      exposed_port: 0,
      limits: { cpu: 0, memory: 0 },
      can_back_up: true,
      attachments: [],
    },
    {
      name: "billing",
      engine: "mariadb",
      var_stem: "DATABASE",
      version: "11",
      status: "running",
      has_container: true,
      username: "cubeship",
      database: "cubeship",
      host: "cubeship-db-billing",
      port: 3306,
      exposed_port: 0,
      limits: { cpu: 0, memory: 0 },
      can_back_up: true,
      attachments: [],
    },
    {
      name: "cache",
      engine: "redis",
      var_stem: "REDIS",
      version: "7",
      status: "running",
      has_container: true,
      username: "default",
      host: "cubeship-db-cache",
      port: 6379,
      exposed_port: 0,
      limits: { cpu: 0, memory: 0 },
      can_back_up: false,
      attachments: [],
    },
  ] as Row[],

  objectStores: [
    {
      name: "uploads",
      kind: "managed",
      provider: "minio",
      provider_label: "MinIO",
      status: "running",
      endpoint: "cubeship-s3-uploads:9000",
      region: "us-east-1",
      path_style: true,
      scopes_by_bucket: false,
      has_container: true,
      exposed_port: 0,
      attachments: [],
    },
    {
      name: "offsite",
      kind: "external",
      provider: "cloudflare",
      provider_label: "Cloudflare R2",
      status: "linked",
      endpoint: "abc123.r2.cloudflarestorage.com",
      region: "auto",
      path_style: true,
      scopes_by_bucket: true,
      bucket: "dumps",
      has_container: false,
      attachments: [],
    },
  ] as Row[],

  backups: [
    {
      id: 3,
      database: "pg",
      database_exists: true,
      engine: "postgres",
      version: "18",
      store: "offsite",
      bucket: "dumps",
      key: "cubeship/pg/2026-09-11T030000Z.dump",
      off_machine: true,
      size_bytes: 48_312_904,
      status: "succeeded",
      scheduled: true,
      started_at: ago(400),
      finished_at: ago(399),
    },
    {
      id: 2,
      database: "pg",
      database_exists: true,
      engine: "postgres",
      version: "18",
      key: "cubeship/pg/2026-09-10T030000Z.dump",
      off_machine: false,
      size_bytes: 47_980_112,
      status: "succeeded",
      scheduled: true,
      started_at: ago(1840),
      finished_at: ago(1839),
    },
    {
      id: 6,
      database: "events",
      database_exists: true,
      engine: "mysql",
      version: "8.4",
      key: "",
      off_machine: false,
      size_bytes: 0,
      status: "failed",
      error: "mysqldump: Got error: 2002: Can't connect to local MySQL server",
      scheduled: true,
      started_at: ago(380),
      finished_at: ago(379),
    },
    {
      id: 5,
      database: "events",
      database_exists: true,
      engine: "mysql",
      version: "8.4",
      store: "offsite",
      bucket: "dumps",
      key: "cubeship/events/2026-09-04T030000Z.dump",
      off_machine: true,
      size_bytes: 12_004_881,
      status: "succeeded",
      scheduled: true,
      started_at: ago(60 * 24 * 7),
      finished_at: ago(60 * 24 * 7 - 1),
    },
    {
      id: 4,
      database: "orders",
      database_exists: true,
      engine: "postgres",
      version: "17",
      key: "cubeship/orders/2026-09-11T030000Z.dump",
      off_machine: false,
      size_bytes: 2_998_110,
      status: "succeeded",
      scheduled: true,
      started_at: ago(410),
      finished_at: ago(409),
    },
    {
      id: 1,
      database: "analytics",
      database_exists: false,
      engine: "mysql",
      version: "8.4",
      store: "offsite",
      bucket: "dumps",
      key: "cubeship/analytics/2026-08-30T030000Z.dump",
      off_machine: true,
      size_bytes: 9_114_002,
      status: "succeeded",
      scheduled: false,
      started_at: ago(60 * 24 * 12),
      finished_at: ago(60 * 24 * 12 - 1),
    },
  ] as Row[],

  backupSchedules: {
    pg: {
      at: "03:00",
      timezone: "America/Sao_Paulo",
      keep: 7,
      store: "offsite",
      bucket: "dumps",
      last_run_at: ago(400),
    },
    // Scheduled, and to this machine's own disk — which is the state
    // the coverage screen exists to call out rather than count as done.
    orders: {
      at: "03:00",
      timezone: "America/Sao_Paulo",
      keep: 7,
      last_run_at: ago(410),
    },
    events: {
      at: "03:00",
      timezone: "America/Sao_Paulo",
      keep: 14,
      store: "offsite",
      bucket: "dumps",
      last_run_at: ago(380),
    },
  } as Record<string, Row>,

  // `GET /registries` answers the logins for registries this instance
  // does not run. Its own is not among them — it has no credential — and
  // every screen that shows it puts it there itself.
  registries: [
    {
      id: 4,
      credential_id: 2,
      provider: "generic",
      host: "ghcr.io",
      username: "acme-bot",
      created_at: ago(60 * 24 * 20),
      updated_at: ago(60 * 24 * 20),
    },
  ] as RegistryCredential[],

  credentials: [
    {
      id: 1,
      label: "Cloudflare",
      username: "",
      created_at: ago(60 * 24 * 40),
      in_use_by: ["DNS: cloudflare"],
    },
    {
      id: 2,
      label: "GHCR",
      username: "acme-bot",
      created_at: ago(60 * 24 * 20),
      in_use_by: ["Registry: ghcr.io"],
    },
  ] as Row[],

  dnsProviders: [
    {
      id: 1,
      provider: "cloudflare",
      provider_name: "Cloudflare",
      credential_id: 1,
      label: "Cloudflare",
      created_at: ago(60 * 24 * 40),
      updated_at: ago(60 * 24 * 40),
    },
    {
      id: 3,
      provider: "route53",
      provider_name: "Route 53",
      credential_id: 3,
      label: "AWS",
      username: "AKIAEXAMPLE",
      created_at: ago(60 * 24 * 9),
      updated_at: ago(60 * 24 * 9),
    },
  ] as DNSProvider[],

  certificates: {
    tls_enabled: true,
    acme_email: "ops@example.com",
    certificates: [
      {
        host: "cubeship.example.com",
        issuer: "R11",
        serial: "03:a1:ff",
        not_before: new Date(now - 35 * 86_400_000).toISOString(),
        not_after: new Date(now + 55 * 86_400_000).toISOString(),
        instance: true,
      },
      {
        host: "api.example.com",
        issuer: "R11",
        serial: "03:b2:0e",
        not_before: new Date(now - 29 * 86_400_000).toISOString(),
        not_after: new Date(now + 61 * 86_400_000).toISOString(),
        app: "web/production/api",
      },
      {
        host: "old.example.com",
        issuer: "R11",
        serial: "03:c0:71",
        not_before: new Date(now - 40 * 86_400_000).toISOString(),
        not_after: new Date(now + 50 * 86_400_000).toISOString(),
        orphan: true,
      },
    ],
    missing: [
      {
        host: "docs.example.com",
        app: "internal/production/docs",
        reason: "pending",
        detail: 'unable to obtain ACME certificate: "docs.example.com" — connection refused',
      },
      { host: "api.staging.example.com", app: "web/staging/api", reason: "not_deployed" },
    ],
    traefik_says: ["too many certificates already issued for example.com"],
  } as CertificateReport,

  firewall: {
    available: true,
    installed: true,
    enabled: true,
    default_incoming: "deny",
    docker_adopted: true,
    ssh_ports: [22],
    ssh_allowed: true,
    your_ip: "198.51.100.7",
    rules: [
      {
        index: 1,
        text: "22/tcp                     ALLOW IN    Anywhere",
        scope: "host",
        action: "allow",
        protocol: "tcp",
        ports: "22",
        comment: "ssh",
        protected: true,
        v6: false,
      },
      {
        index: 2,
        text: "80,443/tcp                 ALLOW IN    Anywhere",
        scope: "host",
        action: "allow",
        protocol: "tcp",
        ports: "80,443",
        protected: false,
        v6: false,
      },
      {
        index: 3,
        text: "5432/tcp                   ALLOW FWD   198.51.100.7",
        scope: "apps",
        action: "allow",
        protocol: "tcp",
        ports: "5432",
        from: "198.51.100.7",
        comment: "psql from the office",
        protected: false,
        v6: false,
      },
    ],
    published: [
      { port: 80, protocol: "tcp", container: "cubeship-traefik", allowed: true },
      { port: 443, protocol: "tcp", container: "cubeship-traefik", allowed: true },
      { port: 3000, protocol: "tcp", container: "cubeship-daemon", allowed: false },
      {
        port: 15000,
        inside: 5432,
        protocol: "tcp",
        container: "cubeship-db-pg",
        allowed: true,
      },
    ],
  } as Firewall,

  nodes: [
    {
      name: "control-plane",
      control_plane: true,
      status: "ready",
      address: "203.0.113.42",
      version: "0.6.0",
      cores: 4,
      memory_total_bytes: 8 * 1024 ** 3,
      disk_total_bytes: 160 * 1024 ** 3,
      cpu_percent: 28,
      memory_bytes: 4.3 * 1024 ** 3,
      in_mesh: true,
      last_seen_at: ago(0),
      created_at: ago(60 * 24 * 200),
    },
    {
      name: "worker-1",
      control_plane: false,
      status: "ready",
      address: "203.0.113.88",
      version: "0.6.0",
      cores: 2,
      memory_total_bytes: 4 * 1024 ** 3,
      disk_total_bytes: 80 * 1024 ** 3,
      cpu_percent: 9,
      memory_bytes: 1.1 * 1024 ** 3,
      in_mesh: true,
      last_seen_at: ago(0),
      created_at: ago(60 * 24 * 30),
    },
  ] as ClusterServer[],

  mesh: { up: true, encrypted: true },

  github: {
    connected: true,
    app_slug: "cubeship-example",
    installations: [{ id: 501, account: "acme" }],
  },

  updates: {
    current: "0.6.0",
    latest: "0.6.0",
    checked: true,
    running: false,
  },
};

// series builds a plausible chart: a slow wave with a little noise, so
// every graph on the preview has a shape rather than a flat line.
export function series(points: number, base: number, swing: number) {
  const out: Row[] = [];
  for (let i = points - 1; i >= 0; i--) {
    const wave = Math.sin((i / points) * Math.PI * 3);
    const noise = Math.sin(i * 12.9898) * 0.5;
    out.push({
      at: new Date(now - i * 60_000).toISOString(),
      value: Math.max(0, base + wave * swing + noise * swing * 0.3),
    });
  }
  return out;
}
