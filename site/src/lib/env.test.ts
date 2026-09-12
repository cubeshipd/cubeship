import { afterEach, describe, expect, it, vi } from "vitest";
import { adminLogins, databaseUrl, publicUrl } from "./env";

afterEach(() => vi.unstubAllEnvs());

describe("env", () => {
  it("throws only when asked, never at import", () => {
    vi.stubEnv("DATABASE_URL", "");
    expect(() => databaseUrl()).toThrow(/DATABASE_URL/);
  });

  it("reads the admin list as lower-case logins", () => {
    vi.stubEnv("ADMIN_LOGINS", " Lucas , someoneElse ");
    expect(adminLogins()).toEqual(["lucas", "someoneelse"]);
  });

  it("falls back to the development origin", () => {
    vi.stubEnv("SITE_URL", "");
    expect(publicUrl()).toBe("http://localhost:3002");
  });
});
