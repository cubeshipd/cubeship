import { afterEach, describe, expect, it, vi } from "vitest";
import { authorizeUrl, exchangeCode, readAccount } from "./github";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("authorizeUrl", () => {
  it("asks for no scope at all and carries the state", () => {
    vi.stubEnv("GITHUB_CLIENT_ID", "abc");
    vi.stubEnv("GITHUB_CLIENT_SECRET", "shh");
    vi.stubEnv("SITE_URL", "https://cubeship.dev");

    const url = new URL(authorizeUrl("s1"));

    expect(url.host).toBe("github.com");
    expect(url.searchParams.get("client_id")).toBe("abc");
    expect(url.searchParams.get("state")).toBe("s1");
    expect(url.searchParams.get("scope")).toBe("");
    expect(url.searchParams.get("redirect_uri")).toBe(
      "https://cubeship.dev/api/auth/github/callback",
    );
  });
});

describe("exchangeCode", () => {
  it("returns the token", async () => {
    vi.stubEnv("GITHUB_CLIENT_ID", "abc");
    vi.stubEnv("GITHUB_CLIENT_SECRET", "shh");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ access_token: "t" })),
    );

    expect(await exchangeCode("c")).toBe("t");
  });

  it("throws when GitHub refuses", async () => {
    vi.stubEnv("GITHUB_CLIENT_ID", "abc");
    vi.stubEnv("GITHUB_CLIENT_SECRET", "shh");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ error: "bad_verification_code" })),
    );

    await expect(exchangeCode("c")).rejects.toThrow(/bad_verification_code/);
  });
});

describe("readAccount", () => {
  it("keeps only the four fields we store", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({ id: 5, login: "lucas", name: "Lucas", avatar_url: "u", email: "x@y.z" }),
      ),
    );

    expect(await readAccount("t")).toEqual({
      id: 5,
      login: "lucas",
      name: "Lucas",
      avatarUrl: "u",
    });
  });
});
