// What each page says is checked against the other product's own site
// and README at the time of writing; a row that cannot be checked says
// so rather than guessing. Keep it that way: a comparison that flatters
// is one nobody trusts on the rows that matter.

export type Cell = { text: string; good?: boolean };

export type Comparison = {
  slug: string;
  name: string;
  title: string;
  description: string;
  intro: string;
  rows: { feature: string; cubeship: Cell; other: Cell }[];
  pickThem: string[];
  pickUs: string[];
  checked: string;
};

const cubeship = {
  license: { text: "Apache-2.0", good: true },
  stack: { text: "Go — one daemon, one binary — with a Next.js dashboard", good: true },
  install: { text: "One command; pulls two images", good: true },
  sources: {
    text: "Push to its own registry (the push is the deploy), an image from any registry, a Dockerfile, or a repository with no Dockerfile (Railpack)",
    good: true,
  },
  compose: { text: "No. One image per app; a database or a bucket is attached to it" },
  templates: { text: "No" },
  git: { text: "GitHub, through a GitHub App the instance registers for itself" },
  previews: { text: "No" },
  databases: { text: "Postgres, MySQL, MariaDB, Redis, MongoDB", good: true },
  backups: {
    text: "Scheduled, streamed to an S3 bucket without touching the disk, restored from the same screen, a coverage report per database, and the instance backs itself up",
    good: true,
  },
  cluster: {
    text: "Workers dial home and publish no port — a box behind NAT joins; an encrypted network between machines; every name arrives at one door",
    good: true,
  },
  scaling: {
    text: "Copies per app, spread over machines; CPU-based autoscaling with a required ceiling",
    good: true,
  },
  zeroDowntime: {
    text: "The new container has to look healthy before the old one goes",
    good: true,
  },
  mcp: {
    text: "Built in: /mcp is the same API for an agent, stateless, with a member's or an admin's role — and two lines no tool crosses",
    good: true,
  },
  api: { text: "OpenAPI document, a reference at /docs, and a CLI", good: true },
  internal: {
    text: "A stable name per app that survives deploys and reaches every copy",
    good: true,
  },
  registry: {
    text: "Built in, behind the instance's own certificate; deleting and reclaiming disk from the dashboard",
    good: true,
  },
  firewall: {
    text: "The host's ufw from the dashboard, aware of the ports Docker opens around it",
    good: true,
  },
  roles: { text: "Admin and member; block, reset, revoke" },
  terminal: { text: "No" },
  notifications: { text: "No" },
  telemetry: { text: "None. Three things leave the box, all documented", good: true },
  cloud: { text: "No hosted version. Self-hosted only" },
};

export const comparisons: Comparison[] = [
  {
    slug: "dokploy",
    name: "Dokploy",
    title: "Cubeship vs Dokploy",
    description:
      "Two self-hosted PaaS, honestly compared: where Dokploy's Compose and templates win, and where Cubeship's registry, backups, workers and MCP do.",
    intro:
      "Both install with one command on a VPS you own, both put Traefik in front of Docker, both run Postgres, MySQL, MariaDB, Redis and MongoDB for you. They differ in what they are built around: Dokploy around Docker Compose, Swarm and templates; Cubeship around one registry, one API, and an agent holding the key.",
    rows: [
      { feature: "License", cubeship: cubeship.license, other: { text: "Apache-2.0", good: true } },
      {
        feature: "Written in",
        cubeship: cubeship.stack,
        other: { text: "TypeScript, on Node.js" },
      },
      {
        feature: "Install",
        cubeship: cubeship.install,
        other: { text: "One command", good: true },
      },
      {
        feature: "Where an app comes from",
        cubeship: cubeship.sources,
        other: {
          text: "Nixpacks, Heroku Buildpacks, a Dockerfile, or a Docker Compose file",
          good: true,
        },
      },
      {
        feature: "Docker Compose",
        cubeship: cubeship.compose,
        other: { text: "Native support", good: true },
      },
      {
        feature: "One-click templates",
        cubeship: cubeship.templates,
        other: {
          text: "Open-source templates — Plausible, PocketBase, Cal.com and more",
          good: true,
        },
      },
      {
        feature: "Git providers",
        cubeship: cubeship.git,
        other: { text: "GitHub, GitLab, Bitbucket, Gitea", good: true },
      },
      {
        feature: "Preview deployments",
        cubeship: cubeship.previews,
        other: { text: "Yes", good: true },
      },
      {
        feature: "Databases",
        cubeship: cubeship.databases,
        other: { text: "Postgres, MySQL, MariaDB, Redis, MongoDB, libsql", good: true },
      },
      {
        feature: "Backups",
        cubeship: cubeship.backups,
        other: { text: "Automated, to an external storage destination" },
      },
      {
        feature: "More than one machine",
        cubeship: cubeship.cluster,
        other: { text: "Docker Swarm clusters, and deploying to remote servers" },
      },
      { feature: "Scaling", cubeship: cubeship.scaling, other: { text: "Swarm replicas" } },
      {
        feature: "Zero-downtime deploys",
        cubeship: cubeship.zeroDowntime,
        other: { text: "Yes, through Swarm's rolling updates", good: true },
      },
      {
        feature: "Agents and MCP",
        cubeship: cubeship.mcp,
        other: { text: "AI-assisted deployments via MCP are advertised on its site" },
      },
      {
        feature: "API and CLI",
        cubeship: cubeship.api,
        other: { text: "API and CLI", good: true },
      },
      {
        feature: "Reaching an app from another app",
        cubeship: cubeship.internal,
        other: { text: "Docker's own network names" },
      },
      {
        feature: "Registry",
        cubeship: cubeship.registry,
        other: { text: "Uses registries you connect" },
      },
      { feature: "Firewall", cubeship: cubeship.firewall, other: { text: "Not managed" } },
      {
        feature: "Users",
        cubeship: cubeship.roles,
        other: { text: "Multiple users with per-project permissions", good: true },
      },
      {
        feature: "Terminal in the browser",
        cubeship: cubeship.terminal,
        other: { text: "Yes", good: true },
      },
      {
        feature: "Notifications",
        cubeship: cubeship.notifications,
        other: { text: "Slack, Discord, Telegram, email", good: true },
      },
      {
        feature: "Monitoring",
        cubeship: { text: "Every container and the machine, charted, kept a day", good: true },
        other: { text: "CPU, memory, storage and network per resource, with alerts", good: true },
      },
      {
        feature: "Telemetry",
        cubeship: cubeship.telemetry,
        other: { text: "Its FAQ addresses usage tracking; check the current answer there" },
      },
      { feature: "A hosted version", cubeship: cubeship.cloud, other: { text: "Dokploy Cloud" } },
    ],
    pickThem: [
      "Your apps are Docker Compose stacks and you want to deploy them as they are.",
      "You want a catalogue of open-source apps to deploy with one click.",
      "You need preview deployments per pull request, or GitLab and Bitbucket.",
      "You want deploy notifications in Slack or Telegram, and a terminal in the browser.",
      "You already run Docker Swarm and want the platform to speak it.",
    ],
    pickUs: [
      "An agent — Claude Code, Cursor, your own — should be able to run the instance through a first-class, role-bound MCP endpoint.",
      "You want docker push to be the deploy, into a registry the instance runs for you.",
      "Backups have to be a report you can trust — off the machine or not, failing or not, per database — and the instance itself must be backed up.",
      "A second machine should join by dialling home, with no port open to the internet, and every name should stay on one address.",
      "You want one Go binary, no telemetry, and the host's firewall in the same dashboard.",
    ],
    checked: "Dokploy's site and README, September 2026",
  },
  {
    slug: "coolify",
    name: "Coolify",
    title: "Cubeship vs Coolify",
    description:
      "Two self-hosted alternatives to Heroku, honestly compared: where Coolify's 280 services, previews and terminal win, and where Cubeship's registry, backups, workers and MCP do.",
    intro:
      "Both are open source under the same license, both install on a VPS you own, both reach other machines and put a proxy in front of Docker. Coolify is the broad one — hundreds of one-click services, Compose, previews, four Git hosts, a terminal. Cubeship is the narrow one: a registry, an API, a cluster, and an agent that can run all of it.",
    rows: [
      { feature: "License", cubeship: cubeship.license, other: { text: "Apache-2.0", good: true } },
      { feature: "Written in", cubeship: cubeship.stack, other: { text: "PHP, on Laravel" } },
      {
        feature: "Install",
        cubeship: cubeship.install,
        other: { text: "One command", good: true },
      },
      {
        feature: "Where an app comes from",
        cubeship: cubeship.sources,
        other: {
          text: "Nixpacks, a Dockerfile, a Docker Compose file, or a static site",
          good: true,
        },
      },
      { feature: "Docker Compose", cubeship: cubeship.compose, other: { text: "Yes", good: true } },
      {
        feature: "One-click services",
        cubeship: cubeship.templates,
        other: { text: "280+", good: true },
      },
      {
        feature: "Git providers",
        cubeship: cubeship.git,
        other: { text: "GitHub, GitLab, Bitbucket, Gitea, hosted or self-hosted", good: true },
      },
      {
        feature: "Preview deployments",
        cubeship: cubeship.previews,
        other: { text: "Per commit and per pull request", good: true },
      },
      {
        feature: "Databases",
        cubeship: cubeship.databases,
        other: { text: "Postgres, MySQL, MariaDB, Redis, MongoDB, and more", good: true },
      },
      {
        feature: "Backups",
        cubeship: cubeship.backups,
        other: { text: "Automatic, to any S3-compatible storage", good: true },
      },
      {
        feature: "More than one machine",
        cubeship: cubeship.cluster,
        other: { text: "Any server it can reach over SSH — VPS, bare metal, a Raspberry Pi" },
      },
      {
        feature: "Scaling",
        cubeship: cubeship.scaling,
        other: { text: "Per server; no autoscaling" },
      },
      {
        feature: "Zero-downtime deploys",
        cubeship: cubeship.zeroDowntime,
        other: { text: "Rolling updates with health checks", good: true },
      },
      {
        feature: "Agents and MCP",
        cubeship: cubeship.mcp,
        other: { text: "Not part of the product; the API is the way in for automation" },
      },
      { feature: "API and CLI", cubeship: cubeship.api, other: { text: "A REST API" } },
      {
        feature: "Reaching an app from another app",
        cubeship: cubeship.internal,
        other: { text: "Docker's own network names" },
      },
      {
        feature: "Registry",
        cubeship: cubeship.registry,
        other: { text: "Uses registries you connect" },
      },
      { feature: "Firewall", cubeship: cubeship.firewall, other: { text: "Not managed" } },
      {
        feature: "Users",
        cubeship: cubeship.roles,
        other: { text: "Teams, with roles", good: true },
      },
      {
        feature: "Terminal in the browser",
        cubeship: cubeship.terminal,
        other: { text: "Yes, on any server", good: true },
      },
      {
        feature: "Notifications",
        cubeship: cubeship.notifications,
        other: { text: "Discord, Telegram, email", good: true },
      },
      {
        feature: "Proxy",
        cubeship: { text: "Traefik, with retries between copies and health checks per app" },
        other: { text: "Traefik or Caddy, your choice", good: true },
      },
      {
        feature: "Telemetry",
        cubeship: cubeship.telemetry,
        other: { text: "Not stated on its site; check its docs" },
      },
      { feature: "A hosted version", cubeship: cubeship.cloud, other: { text: "Coolify Cloud" } },
    ],
    pickThem: [
      "You want to deploy from a catalogue — 280 services — or from Docker Compose files as they are.",
      "You need preview deployments per pull request, GitLab or Bitbucket, or a terminal in the browser.",
      "You want deploy notifications in Discord or Telegram.",
      "You would rather choose Caddy over Traefik.",
      "You want the larger community and the longer track record.",
    ],
    pickUs: [
      "An agent should run the instance — a role-bound MCP endpoint is the product, not a plugin.",
      "You want docker push to be the deploy, into a registry the instance runs for you.",
      "Backups have to be a report you can trust — off the machine or not, failing or not, per database — and the instance itself must be backed up.",
      "A second machine should join by dialling home with no port open, and a name should never move when an app does.",
      "You want one Go binary, no telemetry, CPU-based autoscaling, and the host's firewall in the same dashboard.",
    ],
    checked: "Coolify's site and README, September 2026",
  },
];

export function comparison(slug: string): Comparison | undefined {
  return comparisons.find((c) => c.slug === slug);
}
