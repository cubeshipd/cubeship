import { readFileSync } from "node:fs";
import { prCIScope } from "./pr-ci-scope.mjs";

export function ciScope(paths) {
  const all = { core: true, templates: true, dashboard: true, site: true };
  if (
    paths.length === 0 ||
    paths.some(
      (path) =>
        !["core/", "templates/", "dashboard/", "site/"].some((prefix) => path.startsWith(prefix)) &&
        !["install.sh", "uninstall.sh"].includes(path),
    )
  ) {
    return all;
  }

  return prCIScope(paths);
}

if (process.argv[1]?.endsWith("/ci-scope.mjs")) {
  const paths = readFileSync(0, "utf8").split("\0").filter(Boolean);
  for (const [name, changed] of Object.entries(ciScope(paths))) {
    console.log(`${name}=${changed}`);
  }
}
