import { describe, expect, it } from "vitest";
import { umamiRewrite } from "./umami-proxy";

describe("umamiRewrite", () => {
  const base = "http://cubeship-umami-production-web:3000/";

  it("sends the tracker and its endpoint to Umami", () => {
    expect(umamiRewrite("/u/script.js", "", base)).toBe(
      "http://cubeship-umami-production-web:3000/script.js",
    );
    expect(umamiRewrite("/u/api/send", "?x=1", base)).toBe(
      "http://cubeship-umami-production-web:3000/api/send?x=1",
    );
  });

  it("exposes nothing else of Umami", () => {
    expect(umamiRewrite("/u/login", "", base)).toBeUndefined();
    expect(umamiRewrite("/u/api/auth/login", "", base)).toBeUndefined();
    expect(umamiRewrite("/script.js", "", base)).toBeUndefined();
  });

  it("collects nothing with no Umami configured", () => {
    expect(umamiRewrite("/u/script.js", "", "")).toBeUndefined();
  });
});
