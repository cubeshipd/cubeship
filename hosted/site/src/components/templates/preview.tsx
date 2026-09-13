import { Box, Database, Globe, HardDrive, Link2 } from "lucide-react";
import type { ReactNode } from "react";
import type { NormalizedApp, NormalizedManifest } from "@/lib/manifest";

const ENGINES: Record<string, string> = {
  postgres: "Postgres",
  mysql: "MySQL",
  mariadb: "MariaDB",
  redis: "Redis",
  mongodb: "MongoDB",
};

export function sourceOf(app: NormalizedApp): string {
  return app.source.type === "image"
    ? `${app.source.image}:${app.source.tag ?? "latest"}`
    : app.source.repo;
}

export function engineOf(database: NormalizedManifest["databases"][number]): string {
  const name = ENGINES[database.engine] ?? database.engine;
  return database.version ? `${name} ${database.version}` : name;
}

// What an install ends up with, one row per thing, in the order it is
// created on an instance's own screens: apps, then what they attach.
export function Preview({ manifest }: { manifest: NormalizedManifest }) {
  const nameOf = (kind: "database" | "store", key: string) =>
    (kind === "database" ? manifest.databases : manifest.stores).find((item) => item.key === key)
      ?.name ?? key;

  return (
    <div className="hud-frame divide-y divide-fd-border border border-fd-border">
      {manifest.apps.map((app) => (
        <Row key={`app-${app.key}`} icon={<Box />} name={app.name} detail={sourceOf(app)}>
          {app.port ? <Chip>port {app.port}</Chip> : null}
          {app.domains.length > 0 ? (
            <Chip icon={<Globe />}>
              {app.domains.length > 1 ? `${app.domains.length} domains` : "domain"}
            </Chip>
          ) : null}
          {app.attach.map((attach) => (
            <Chip key={`${attach.kind}-${attach.key}-${attach.prefix}`} icon={<Link2 />}>
              {nameOf(attach.kind, attach.key)}
            </Chip>
          ))}
        </Row>
      ))}
      {manifest.databases.map((database) => (
        <Row
          key={`db-${database.key}`}
          icon={<Database />}
          name={database.name}
          detail={engineOf(database)}
        />
      ))}
      {manifest.stores.map((store) => (
        <Row
          key={`store-${store.key}`}
          icon={<HardDrive />}
          name={store.name}
          detail={store.buckets.length > 0 ? store.buckets.join(", ") : "Object storage"}
        />
      ))}
    </div>
  );
}

function Row({
  icon,
  name,
  detail,
  children,
}: {
  icon: ReactNode;
  name: string;
  detail: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-4 px-4 py-3">
      <span className="flex size-9 shrink-0 items-center justify-center border border-fd-border text-primary [&_svg]:size-4">
        {icon}
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate font-mono text-fd-foreground text-sm">{name}</p>
        <p className="truncate font-mono text-fd-muted-foreground text-xs">{detail}</p>
      </div>
      {children ? (
        <div className="hidden shrink-0 flex-wrap justify-end gap-2 sm:flex">{children}</div>
      ) : null}
    </div>
  );
}

function Chip({ icon, children }: { icon?: ReactNode; children: ReactNode }) {
  return (
    <span className="label inline-flex items-center gap-1.5 border border-fd-border px-2 py-1 text-fd-muted-foreground [&_svg]:size-3">
      {icon}
      {children}
    </span>
  );
}
