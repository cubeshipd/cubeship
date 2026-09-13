import { Box, Database, HardDrive } from "lucide-react";
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
  return (
    <div className="hud-frame divide-y divide-fd-border border border-fd-border">
      {manifest.apps.map((app) => (
        <Row key={`app-${app.key}`} icon={<Box />} name={app.name} detail={sourceOf(app)} />
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

function Row({ icon, name, detail }: { icon: ReactNode; name: string; detail: string }) {
  return (
    <div className="flex items-center gap-4 px-4 py-3">
      <span className="flex size-9 shrink-0 items-center justify-center border border-fd-border text-primary [&_svg]:size-4">
        {icon}
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate font-mono text-fd-foreground text-sm">{name}</p>
        <p className="truncate font-mono text-fd-muted-foreground text-xs">{detail}</p>
      </div>
    </div>
  );
}
