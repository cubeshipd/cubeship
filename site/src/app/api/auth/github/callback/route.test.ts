import { beforeEach, describe, expect, it, vi } from "vitest";

const store = new Map<string, string>();
vi.mock("next/headers", () => ({
  cookies: async () => ({
    get: (name: string) => (store.has(name) ? { name, value: store.get(name) } : undefined),
    set: (name: string, value: string) => void store.set(name, value),
    delete: (name: string) => void store.delete(name),
  }),
}));

// A mismatch must refuse before anything reaches GitHub or the database.
const exchangeCode = vi.fn();
vi.mock("@/lib/auth/github", () => ({
  STATE_COOKIE: "cubeship_oauth_state",
  exchangeCode,
  readAccount: vi.fn(),
}));

beforeEach(() => {
  store.clear();
  exchangeCode.mockClear();
});

describe("the callback", () => {
  it("refuses a state that does not match the cookie", async () => {
    const { GET } = await import("./route");
    store.set("cubeship_oauth_state", "expected:/templates");

    const response = await GET(
      new Request("https://cubeship.dev/api/auth/github/callback?code=c&state=wrong"),
    );

    expect(response.status).toBe(400);
    expect((await response.json()).error.code).toBe("bad_state");
    expect(exchangeCode).not.toHaveBeenCalled();
  });

  it("refuses when there is no state cookie at all", async () => {
    const { GET } = await import("./route");

    const response = await GET(
      new Request("https://cubeship.dev/api/auth/github/callback?code=c&state=s"),
    );

    expect(response.status).toBe(400);
    expect(exchangeCode).not.toHaveBeenCalled();
  });
});
