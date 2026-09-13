// cubeship.dev/api/v1 is the catalog's public address: instances pin it,
// and the catalog behind it can move without them noticing. The target
// is read per request, never baked into the build the way next.config
// rewrites are — the image is built before anybody knows the address.
export function catalogRewrite(pathname: string, search: string, base: string): string | undefined {
  if (!pathname.startsWith("/api/v1/")) return undefined;
  return `${base.replace(/\/$/, "")}/v1/${pathname.slice("/api/v1/".length)}${search}`;
}
