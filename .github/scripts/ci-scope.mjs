import { readFileSync } from "node:fs";

export function needsFullCI(paths) {
  return paths.length === 0 || paths.some((path) => !path.startsWith("templates/"));
}

if (process.argv[1]?.endsWith("/ci-scope.mjs")) {
  const paths = readFileSync(0, "utf8").split("\0").filter(Boolean);
  console.log(`full=${needsFullCI(paths)}`);
}
