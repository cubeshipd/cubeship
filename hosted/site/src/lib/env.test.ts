import { afterEach, describe, expect, it, vi } from "vitest";
import { catalogUrl } from "./env";

afterEach(() => vi.unstubAllEnvs());

describe("catalogUrl", () => {
  it("falls back to the catalog `make catalog-dev` runs", () => {
    vi.stubEnv("CATALOG_URL", "");
    expect(catalogUrl()).toBe("http://localhost:8080");
  });

  it("reads the address at call time", () => {
    vi.stubEnv("CATALOG_URL", "http://cubeship-cubeship-production-cubeship-catalog:8080");
    expect(catalogUrl()).toBe("http://cubeship-cubeship-production-cubeship-catalog:8080");
  });
});
