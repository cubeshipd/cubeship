import type { MetadataRoute } from "next";
import { comparisons } from "@/lib/comparisons";
import { withDeadline } from "@/lib/deadline";
import { siteUrl } from "@/lib/shared";
import { source } from "@/lib/source";
import { allPublishedTemplates } from "@/lib/templates/queries";

// Reads Postgres for the templates, so this can never be static — see
// the `force-dynamic` below. The query lives in the function body,
// never at module scope, which is what keeps `next build` from needing
// a database: nothing here runs until a request asks for the sitemap.
export const dynamic = "force-dynamic";

// The landing page and every docs page there is, from the same tree the
// sidebar is built from — a page added to the docs is in the sitemap
// without anybody remembering it. Templates are the one part of this
// that changes without a deploy, so they are read fresh every time.
export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const docs = source.getPages().map((page) => ({
    url: `${siteUrl}${page.url}`,
    changeFrequency: "weekly" as const,
    priority: page.url === "/docs" ? 0.9 : 0.7,
  }));
  const vs = comparisons.map((c) => ({
    url: `${siteUrl}/vs/${c.slug}`,
    changeFrequency: "monthly" as const,
    priority: 0.8,
  }));
  // A database that is down still leaves the landing page and the docs
  // worth listing; only the templates are missing until it is back.
  let publishedTemplates: Awaited<ReturnType<typeof allPublishedTemplates>> = [];
  try {
    publishedTemplates = await withDeadline(allPublishedTemplates(), 1_500, "sitemap templates");
  } catch (error) {
    console.warn("sitemap: templates unavailable:", (error as Error).message);
  }
  const templates = publishedTemplates.map((template) => ({
    url: `${siteUrl}/templates/${template.slug}`,
    lastModified: template.updatedAt,
    changeFrequency: "weekly" as const,
    priority: 0.6,
  }));

  return [
    { url: siteUrl, changeFrequency: "weekly", priority: 1 },
    { url: `${siteUrl}/templates`, changeFrequency: "daily", priority: 0.8 },
    ...vs,
    ...docs,
    ...templates,
  ];
}
