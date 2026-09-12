import {
  Activity,
  Archive,
  Bot,
  Database,
  Hammer,
  HardDrive,
  LayoutDashboard,
  Lock,
  Network,
  Package,
  RefreshCw,
  Server,
} from "lucide-react";
import type { ReactNode } from "react";
import { Section } from "./section";

const features: { icon: ReactNode; title: string; body: string }[] = [
  {
    icon: <LayoutDashboard />,
    title: "A dashboard",
    body: "At your instance's address, over HTTPS, from the minute the installer finishes.",
  },
  {
    icon: <Package />,
    title: "A registry",
    body: "docker push to it and the app deploys. The push is the deploy — nothing else to press.",
  },
  {
    icon: <RefreshCw />,
    title: "Zero-downtime deploys",
    body: "The new container has to look healthy before the old one goes.",
  },
  {
    icon: <Lock />,
    title: "Certificates",
    body: "Let's Encrypt, renewed for you, nothing to configure. An sslip.io name until you have one.",
  },
  {
    icon: <Database />,
    title: "Databases",
    body: "Postgres, MySQL, MariaDB, Redis, MongoDB — one click, wired into the app's environment.",
  },
  {
    icon: <Archive />,
    title: "Backups",
    body: "Every database dumped on a schedule into a bucket off the machine, restored from the same screen.",
  },
  {
    icon: <HardDrive />,
    title: "Object storage",
    body: "A MinIO on the box, or the S3 bucket you already have. The app gets the keys.",
  },
  {
    icon: <Hammer />,
    title: "Builds",
    body: "From a Dockerfile, or from a repository with no Dockerfile at all. A push to GitHub deploys it.",
  },
  {
    icon: <Server />,
    title: "More machines",
    body: "Add a second server and apps spread across both. Every name still arrives at one door.",
  },
  {
    icon: <Network />,
    title: "Internal addresses",
    body: "cubeship-<project>-<env>-<app> reaches an app from any other, on any machine, across deploys.",
  },
  {
    icon: <Activity />,
    title: "Charts",
    body: "What every container is using, and what the machine underneath is doing.",
  },
  {
    icon: <Bot />,
    title: "An API, a CLI and MCP",
    body: "Everything the dashboard does, scriptable — and an endpoint an agent drives directly.",
  },
];

export function Features() {
  return (
    <Section
      id="what-you-get"
      label="What you get"
      title="One command on a fresh VPS, and the box is running."
      lede="Everything Cubeship runs is a container, the daemon included. Nothing else is installed on the host."
    >
      <ul className="grid gap-px border border-border bg-border sm:grid-cols-2 lg:grid-cols-3">
        {features.map((f) => (
          <li key={f.title} className="bg-background p-6">
            <div className="text-primary [&>svg]:size-5">{f.icon}</div>
            <h3 className="mt-4 font-semibold text-base">{f.title}</h3>
            <p className="mt-2 text-muted-foreground text-sm leading-relaxed">{f.body}</p>
          </li>
        ))}
      </ul>
    </Section>
  );
}
