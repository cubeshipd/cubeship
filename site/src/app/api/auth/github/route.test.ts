import { describe, expect, it, vi } from "vitest";

const store = new Map<string, string>();
vi.mock("next/headers", () => ({
  cookies: async () => ({
    get: (name: string) => (store.has(name) ? { name, value: store.get(name) } : undefined),
    set: (name: string, value: string) => void store.set(name, value),
    delete: (name: string) => void store.delete(name),
  }),
}));

vi.mock("@/lib/auth/github", () => ({
  STATE_COOKIE: "cubeship_oauth_state",
  authorizeUrl: (state: string) => `https://github.com/login/oauth/authorize?state=${state}`,
}));

describe("starting sign-in", () => {
  it("keeps a same-site next path", async () => {
    const { GET } = await import("./route");
    store.clear();

    await GET(new Request("https://cubeship.dev/api/auth/github?next=/u/lucas"));

    expect(store.get("cubeship_oauth_state")).toMatch(/:\/u\/lucas$/);
  });

  it("refuses to carry an absolute URL as next", async () => {
    const { GET } = await import("./route");
    store.clear();

    await GET(new Request("https://cubeship.dev/api/auth/github?next=https://evil.example/"));

    expect(store.get("cubeship_oauth_state")).toMatch(/:\/templates$/);
  });

  it("refuses a protocol-relative next", async () => {
    const { GET } = await import("./route");
    store.clear();

    await GET(new Request("https://cubeship.dev/api/auth/github?next=//evil.example/"));

    expect(store.get("cubeship_oauth_state")).toMatch(/:\/templates$/);
  });
});
