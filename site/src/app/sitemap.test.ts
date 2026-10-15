import { describe, expect, it, vi } from "vitest";

// source.ts pulls in fumadocs-mdx's build-time macro, which only works
// under Next's own bundler — stub it with the one page sitemap.ts reads.
vi.mock("@/lib/source", () => ({
  source: { getPages: () => [{ url: "/docs" }] },
}));
vi.mock("@/lib/comparisons", () => ({ comparisons: [] }));
vi.mock("@/lib/catalog", () => ({
  allTemplates: vi.fn().mockRejectedValue(new Error("connect ECONNREFUSED 10.0.0.1:5432")),
}));

describe("sitemap", () => {
  it("falls back to the static entries when the templates query fails", async () => {
    const sitemap = (await import("./sitemap")).default;
    const entries = await sitemap();

    expect(entries.some((entry) => entry.url.includes("/templates/"))).toBe(false);
    expect(entries.some((entry) => entry.url.endsWith("/docs"))).toBe(true);
  });
});
