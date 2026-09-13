import type { Diagnostic, Locate } from "./diagnostics";
import { findReferences } from "./references";
import type { Manifest } from "./schema";

type Path = (string | number)[];

export function advise(manifest: Manifest, locate: Locate): Diagnostic[] {
  const found: Diagnostic[] = [];
  const say = (severity: "warning" | "info", code: string, message: string, path: Path) => {
    found.push({
      severity,
      code,
      message,
      path,
      range: locate(path, "key") ?? locate(path.slice(0, -1)),
    });
  };

  manifest.databases.forEach((database, index) => {
    if (!database.version) {
      say(
        "warning",
        "advice.unpinned-engine",
        "without a version, two installs of this template run different engines",
        ["databases", index, "engine"],
      );
    }
  });

  // An app nothing addresses is usually a mistake: it has no domain and
  // no other app names it.
  const referenced = new Set(
    manifest.apps.flatMap((app) =>
      Object.values(app.env).flatMap((value) =>
        findReferences(value)
          .filter((r) => r.kind === "app")
          .map((r) => r.key),
      ),
    ),
  );

  manifest.apps.forEach((app, index) => {
    const at = (...rest: Path): Path => ["apps", index, ...rest];

    if (!app.health) {
      say(
        "warning",
        "advice.no-health",
        "with no health path, a deploy cannot tell whether this started",
        at("key"),
      );
    }
    if (app.image && !app.tag) {
      say(
        "warning",
        "advice.floating-tag",
        "without a tag this follows the registry: two installs may differ",
        at("image"),
      );
    }
    if (app.image && !app.image.includes("/")) {
      say(
        "info",
        "advice.dockerhub",
        "an unqualified image comes from Docker Hub, which rate-limits anonymous pulls",
        at("image"),
      );
    }
    if (!app.limits) {
      say(
        "warning",
        "advice.no-limits",
        "with no limits, one copy of this app can take the whole machine",
        at("key"),
      );
    }
    if (app.domains.length === 0 && !referenced.has(app.key)) {
      say(
        "warning",
        "advice.unreachable-app",
        "nothing reaches this app: it has no domain and no other app names it",
        at("key"),
      );
    }
  });

  return found;
}
