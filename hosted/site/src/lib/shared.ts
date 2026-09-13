import { createGetUrl } from "fumadocs-core/source";

export const appName = "Cubeship";
export const siteUrl = "https://cubeship.dev";
export const docsRoute = "/docs";
export const docsImageRoute = "/og/docs";
export const docsContentRoute = "/llms.mdx/docs";
// cubeship.dev in Umami. Not a secret: it is in every page's HTML.
export const umamiWebsiteId = "9db42214-19a1-4e99-a18d-597502873f40";

export const gitConfig = {
  user: "cubeshipd",
  repo: "cubeship",
  branch: "master",
};
export const githubUrl = `https://github.com/${gitConfig.user}/${gitConfig.repo}`;

const getContentUrl = createGetUrl(docsContentRoute);

export function getPageMarkdownUrl(page: { slugs: string[]; locale?: string }) {
  const segments = [...page.slugs, "content.md"];

  return { segments, url: getContentUrl(segments, page.locale) };
}

const getImageUrl = createGetUrl(docsImageRoute);

export function getPageImageUrl(page: { slugs: string[]; locale?: string }) {
  const segments = [...page.slugs, "image.png"];

  return { segments, url: getImageUrl(segments, page.locale) };
}
