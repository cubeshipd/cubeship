import { describe, expect, it } from "vitest";
import { catalogRewrite } from "./catalog-proxy";

describe("catalogRewrite", () => {
  it("sends /api/v1 to the catalog's /v1, query and all", () => {
    expect(catalogRewrite("/api/v1/templates", "?sort=stars", "http://catalog:8080/")).toBe(
      "http://catalog:8080/v1/templates?sort=stars",
    );
  });

  it("leaves everything else to the site", () => {
    expect(catalogRewrite("/api/search", "", "http://catalog:8080")).toBeUndefined();
    expect(catalogRewrite("/api/v10/x", "", "http://catalog:8080")).toBeUndefined();
    expect(catalogRewrite("/templates", "", "http://catalog:8080")).toBeUndefined();
  });
});
