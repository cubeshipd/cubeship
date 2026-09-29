// A README is written to be read on GitHub, so its relative links and
// images are relative to the repository. Here they are resolved to the
// commit the template was read at, so a page never shows a newer image
// than the file it describes.
export function resolveReadmeUrl(
  url: string,
  kind: "image" | "link",
  where: { owner: string; repo: string; commit: string },
): string {
  if (
    url === "" ||
    url.startsWith("#") ||
    /^[a-z][a-z0-9+.-]*:/i.test(url) ||
    url.startsWith("//")
  ) {
    return url;
  }
  const path = url.replace(/^\.?\//, "");
  return kind === "image"
    ? `https://raw.githubusercontent.com/cubeshipd/cubeship-templates/${where.commit}/${where.repo}/${path}`
    : `https://github.com/cubeshipd/cubeship-templates/blob/${where.commit}/${where.repo}/${path}`;
}
