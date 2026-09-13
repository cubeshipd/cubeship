import type { ReactNode } from "react";
import type { NormalizedApp, NormalizedManifest } from "@/lib/manifest";

// Only a domain input's own reference is worth spelling out by label;
// anything else is either not there yet or not something a template may
// carry (see docs/design/hosted/templates.md: "No domain").
function domainInputKey(host: string): string | undefined {
  return /^\$\{input\.([a-zA-Z][a-zA-Z0-9_-]*)\}$/.exec(host)?.[1];
}

function describeDomain(host: string, inputs: NormalizedManifest["inputs"]): string {
  const key = domainInputKey(host);
  const input = key ? inputs.find((candidate) => candidate.key === key) : undefined;
  return input ? `at the domain from “${input.label}”` : `at ${host}`;
}

export function describeApp(app: NormalizedApp, inputs: NormalizedManifest["inputs"]): string {
  const source =
    app.source.type === "image"
      ? `${app.source.image}:${app.source.tag ?? "latest"}`
      : `built from ${app.source.repo}`;
  const port = app.port ? ` on port ${app.port}` : "";
  const domain =
    app.domains.length === 0
      ? "with no public domain"
      : app.domains.length === 1
        ? describeDomain(app.domains[0].host, inputs)
        : `at ${app.domains.length} domains`;
  const internal = app.port ? `${app.internal_host}:${app.port}` : app.internal_host;

  return `${source}${port}, ${domain}, reachable inside as ${internal}`;
}

export function describeDatabase(database: NormalizedManifest["databases"][number]): string {
  const port = database.port ?? "its default port";
  return `${database.engine} ${database.version ?? "latest"}, named ${database.name}, on port ${port}`;
}

function describeStore(store: NormalizedManifest["stores"][number]): string {
  const buckets = store.buckets.length > 0 ? store.buckets.join(", ") : "no buckets yet";
  return `object storage, named ${store.name}, with ${buckets}`;
}

function describeAttachment(
  attach: NormalizedApp["attach"][number],
  manifest: NormalizedManifest,
): string {
  if (attach.kind === "database") {
    const database = manifest.databases.find((candidate) => candidate.key === attach.key);
    return `the database ${database?.name ?? attach.key}${attach.prefix ? ` at prefix "${attach.prefix}"` : ""}`;
  }
  const store = manifest.stores.find((candidate) => candidate.key === attach.key);
  const bucket = attach.bucket ? ` bucket "${attach.bucket}"` : "";
  return `the store ${store?.name ?? attach.key}${bucket}${attach.prefix ? ` at prefix "${attach.prefix}"` : ""}`;
}

function Block({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="hud-frame border border-fd-border">
      <p className="label border-fd-border border-b px-4 py-2 text-fd-muted-foreground">{label}</p>
      <div className="divide-y divide-fd-border">{children}</div>
    </div>
  );
}

// What the file will create, in the order the design doc renders it:
// apps, databases, stores, then the inputs an installer must answer.
export function Preview({ manifest }: { manifest: NormalizedManifest }) {
  return (
    <div className="space-y-4">
      <Block label={`Apps — project “${manifest.project}”`}>
        {manifest.apps.map((app) => (
          <div key={app.key} className="px-4 py-3 text-sm">
            <p className="font-mono text-fd-foreground">{app.name}</p>
            <p className="mt-1 text-fd-muted-foreground">{describeApp(app, manifest.inputs)}</p>
            {app.attach.length > 0 ? (
              <ul className="mt-2 space-y-1 text-fd-muted-foreground text-xs">
                {app.attach.map((attach) => (
                  <li key={`${attach.kind}-${attach.key}-${attach.prefix}-${attach.bucket ?? ""}`}>
                    attaches {describeAttachment(attach, manifest)}
                  </li>
                ))}
              </ul>
            ) : null}
          </div>
        ))}
      </Block>

      {manifest.databases.length > 0 ? (
        <Block label="Databases">
          {manifest.databases.map((database) => (
            <p key={database.key} className="px-4 py-3 text-fd-muted-foreground text-sm">
              {describeDatabase(database)}
            </p>
          ))}
        </Block>
      ) : null}

      {manifest.stores.length > 0 ? (
        <Block label="Object storage">
          {manifest.stores.map((store) => (
            <p key={store.key} className="px-4 py-3 text-fd-muted-foreground text-sm">
              {describeStore(store)}
            </p>
          ))}
        </Block>
      ) : null}

      {manifest.inputs.length > 0 ? (
        <Block label="What an installer fills in">
          {manifest.inputs.map((input) => (
            <p key={input.key} className="px-4 py-3 text-fd-muted-foreground text-sm">
              <span className="font-mono text-fd-foreground">{input.label}</span> — {input.type}
              {input.required === false ? ", optional" : ""}
              {input.help ? <span className="mt-1 block text-xs">{input.help}</span> : null}
            </p>
          ))}
        </Block>
      ) : null}
    </div>
  );
}
