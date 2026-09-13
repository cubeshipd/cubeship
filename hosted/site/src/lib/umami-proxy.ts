// Umami's tracker and the endpoint it reports to, served from the site's
// own address: the browser never talks to another host, so a blocker that
// knows Umami's domain has nothing to block, and the Umami behind it is
// reached on the instance's internal network. Read per request, like the
// catalog, because the image is built before anybody knows the address.
//
// Only the two paths the tracker uses. The rest of Umami — its login, its
// dashboard — is not the site's to expose.
const paths: Record<string, string> = {
  "/u/script.js": "/script.js",
  "/u/api/send": "/api/send",
};

export function umamiRewrite(pathname: string, search: string, base: string): string | undefined {
  const target = paths[pathname];
  if (!target || !base) return undefined;
  return `${base.replace(/\/$/, "")}${target}${search}`;
}
