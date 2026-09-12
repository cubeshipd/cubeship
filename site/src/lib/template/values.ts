// Copied from the daemon rather than fetched: a template is validated
// here before any instance sees it. internal/slug/slug.go is the original.
export const slugPattern = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

export const reserved = {
  slugs: new Set(["settings"]),
  databases: new Set(["settings", "engines"]),
  stores: new Set(["settings", "providers"]),
};

export const envKeyPattern = /^[A-Za-z_][A-Za-z0-9_]*$/;
export const prefixPattern = /^[A-Z][A-Z0-9_]*_$/;
export const tagPattern = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/;

export const MIN_CPU = 0.01;
export const MIN_MEMORY = 6 * 1024 * 1024;
export const MAX_AUTOSCALE = 100;
export const MAX_HEALTH_PATH = 255;
export const DEFAULT_PORT = 8080;

const units: [string, number][] = [
  ["Gi", 1 << 30],
  ["G", 1 << 30],
  ["Mi", 1 << 20],
  ["M", 1 << 20],
  ["Ki", 1 << 10],
  ["K", 1 << 10],
  ["B", 1],
];

// "512M" and "512Mi" are the same request; refusing one would be
// pedantry in front of a limit. cmd/cubeship/app.go reads them the same way.
export function parseSize(input: string | number): number | undefined {
  if (typeof input === "number") return Number.isFinite(input) ? Math.trunc(input) : undefined;

  let rest = input.trim();
  let factor = 1;
  for (const [suffix, value] of units) {
    if (rest.length > suffix.length && rest.toLowerCase().endsWith(suffix.toLowerCase())) {
      factor = value;
      rest = rest.slice(0, -suffix.length);
      break;
    }
  }

  const n = Number(rest.trim());
  if (rest.trim() === "" || !Number.isFinite(n)) return undefined;
  return Math.trunc(n * factor);
}

export function healthPathProblem(path: string): string | undefined {
  if (!path.startsWith("/")) return "a health path has to start with /";
  if (path.length > MAX_HEALTH_PATH)
    return `a health path is at most ${MAX_HEALTH_PATH} characters`;
  if (path.includes("?")) return "a health path carries no query string";
  if (path.includes("#")) return "a health path carries no fragment";
  return undefined;
}
