import type { MetadataRoute } from "next";
import { comparisons } from "@/lib/comparisons";
import { siteUrl } from "@/lib/shared";
import { source } from "@/lib/source";

// The landing page and every docs page there is, from the same tree the
// sidebar is built from — a page added to the docs is in the sitemap
// without anybody remembering it.
export default function sitemap(): MetadataRoute.Sitemap {
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
  return [{ url: siteUrl, changeFrequency: "weekly", priority: 1 }, ...vs, ...docs];
}
