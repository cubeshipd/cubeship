// Which entry of the Registry select an image reference belongs to.
//
// The select is not one entry per registry on earth: it is Cubeship's own,
// every registry this instance holds a login for, Docker Hub, and one
// entry for everything else — a public image needs no login, wherever it
// is published.

// CUBESHIP is the reserved key for the registry this instance runs, the
// same word its id takes everywhere else.
export const CUBESHIP = "cubeship";

// DOCKER_HUB is where an image with no registry in its name comes from.
export const DOCKER_HUB = "index.docker.io";

// PUBLIC_IMAGE is the entry for a public image from any other registry.
// It is a key and not a host: nothing is pulled from a registry named
// "public", which is why it can never collide with one.
export const PUBLIC_IMAGE = "public";

// hasHost is whether the reference names a registry at all, as Docker does:
// the first path segment is a host when it has a dot or a port, or is
// localhost. `acme/api` and `nginx` are Docker Hub's and do not.
export function hasHost(image: string): boolean {
  const first = image.split("/")[0] ?? "";
  return image.includes("/") && (/[.:]/.test(first) || first === "localhost");
}

// hostOf is which registry an image reference names, in the spelling the
// daemon uses: a reference with no registry in it at all is Docker Hub's,
// and so are both of the Hub's own names.
export function hostOf(image: string): string {
  if (!hasHost(image)) return DOCKER_HUB;
  const host = (image.split("/")[0] ?? "").toLowerCase();
  return host === "docker.io" || host === "registry-1.docker.io" ? DOCKER_HUB : host;
}

// entryFor is the select entry for a registry: a login this instance
// holds, Docker Hub, or the public one for any other host.
export function entryFor(registry: string, connected: readonly string[]): string {
  if (registry === CUBESHIP || registry === DOCKER_HUB || connected.includes(registry)) {
    return registry;
  }
  return PUBLIC_IMAGE;
}

// entryForTyped is the entry to show once an image has been typed, which
// is the current one unless the reference names another registry.
//
// Only an explicit host counts. `quay` on the way to `quay.io/acme/api`
// has no host yet, and reading it as Docker Hub's would flip the select
// under the person typing.
export function entryForTyped(
  image: string,
  current: string,
  connected: readonly string[],
): string {
  if (current === CUBESHIP || !hasHost(image)) return current;
  return entryFor(hostOf(image), connected);
}
