// A feature is a tick or nothing. A row is only here when both answers
// could be checked against the other product's own site or README; an
// unknown is left out rather than shown as an absence.

export type Row = { feature: string; cubeship: boolean; other: boolean };
export type Group = { title: string; rows: Row[] };

export type Comparison = {
  slug: string;
  name: string;
  title: string;
  description: string;
  // The one sentence under the title: what the other product charges for.
  cost: string;
  groups: Group[];
  checked: string;
};

const row = (feature: string, cubeship: boolean, other: boolean): Row => ({
  feature,
  cubeship,
  other,
});

export const comparisons: Comparison[] = [
  {
    slug: "dokploy",
    name: "Dokploy",
    title: "Cubeship vs Dokploy",
    description:
      "Feature by feature, Cubeship against Dokploy — and what each one costs, which is nothing.",
    cost: "Dokploy sells plans from $4.50/mo per server, with unlimited users, unlimited environments, unlimited servers, SSO and audit logs behind its tiers.",
    groups: [
      {
        title: "Cost",
        rows: [
          row("Open source", true, true),
          row("Every feature free, no plans", true, false),
          row("Unlimited servers without paying", true, false),
          row("Unlimited users without paying", true, false),
          row("Unlimited environments without paying", true, false),
          row("One-command install on your own server", true, true),
        ],
      },
      {
        title: "Deploying",
        rows: [
          row("From a Dockerfile", true, true),
          row("From a repository with no Dockerfile", true, true),
          row("From an image on any registry", true, true),
          row("Docker Compose stacks", false, true),
          row("One-click templates", false, true),
          row("Built-in registry — docker push is the deploy", true, false),
          row("Deploy on push from GitHub", true, true),
          row("GitLab, Bitbucket, Gitea", false, true),
          row("Preview deployments", false, true),
          row("Zero-downtime deploys", true, true),
          row("Health checks", true, true),
        ],
      },
      {
        title: "Data",
        rows: [
          row("Postgres, MySQL, MariaDB, Redis, MongoDB", true, true),
          row("Scheduled backups to S3", true, true),
          row("Restore from the dashboard", true, true),
          row("Backup coverage report per database", true, false),
          row("Object storage attached to apps, keys injected", true, false),
        ],
      },
      {
        title: "Machines",
        rows: [
          row("More than one server", true, true),
          row("Servers join with no port open to the internet", true, false),
          row("Private network between servers", true, true),
          row("Copies of an app spread over servers", true, true),
          row("CPU-based autoscaling", true, false),
          row("Per-container CPU and memory limits", true, true),
          row("Stable internal address per app", true, false),
          row("Host firewall managed from the dashboard", true, false),
        ],
      },
      {
        title: "Operating",
        rows: [
          row("Charts for every container and the machine", true, true),
          row("Logs in the dashboard", true, true),
          row("Users and roles", true, true),
          row("Certificates issued and renewed", true, true),
          row("API", true, true),
          row("CLI", true, true),
          row("MCP endpoint for agents", true, true),
          row("Terminal in the browser", false, true),
          row("Deploy notifications", false, true),
        ],
      },
    ],
    checked: "Dokploy's site and README, September 2026",
  },
  {
    slug: "coolify",
    name: "Coolify",
    title: "Cubeship vs Coolify",
    description:
      "Feature by feature, Cubeship against Coolify — and what each one costs, which is nothing.",
    cost: "Coolify is free to self-host; what it sells is Coolify Cloud. Cubeship has nothing to sell.",
    groups: [
      {
        title: "Cost",
        rows: [
          row("Open source", true, true),
          row("Free to self-host", true, true),
          row("No paid tier, no hosted upsell", true, false),
          row("One-command install on your own server", true, true),
        ],
      },
      {
        title: "Deploying",
        rows: [
          row("From a Dockerfile", true, true),
          row("From a repository with no Dockerfile", true, true),
          row("From an image on any registry", true, true),
          row("Static sites", false, true),
          row("Docker Compose stacks", false, true),
          row("One-click services", false, true),
          row("Built-in registry — docker push is the deploy", true, false),
          row("Deploy on push from GitHub", true, true),
          row("GitLab, Bitbucket, Gitea", false, true),
          row("Preview deployments", false, true),
          row("Zero-downtime deploys", true, true),
          row("Health checks", true, true),
        ],
      },
      {
        title: "Data",
        rows: [
          row("Postgres, MySQL, MariaDB, Redis, MongoDB", true, true),
          row("Scheduled backups to S3", true, true),
          row("Restore from the dashboard", true, true),
          row("Backup coverage report per database", true, false),
          row("Backs up the platform itself", true, true),
          row("Object storage attached to apps, keys injected", true, false),
        ],
      },
      {
        title: "Machines",
        rows: [
          row("More than one server", true, true),
          row("Servers join with no port open to the internet", true, false),
          row("Private network between servers", true, false),
          row("Copies of an app spread over servers", true, false),
          row("CPU-based autoscaling", true, false),
          row("Per-container CPU and memory limits", true, true),
          row("Stable internal address per app", true, false),
          row("Host firewall managed from the dashboard", true, false),
        ],
      },
      {
        title: "Operating",
        rows: [
          row("Charts for every container and the machine", true, true),
          row("Logs in the dashboard", true, true),
          row("Users and roles", true, true),
          row("Certificates issued and renewed", true, true),
          row("API", true, true),
          row("CLI", true, false),
          row("MCP endpoint for agents", true, false),
          row("Terminal in the browser", false, true),
          row("Deploy notifications", false, true),
          row("Choice of proxy", false, true),
        ],
      },
    ],
    checked: "Coolify's site and README, September 2026",
  },
];

export function comparison(slug: string): Comparison | undefined {
  return comparisons.find((c) => c.slug === slug);
}
