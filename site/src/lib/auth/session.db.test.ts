import { describe, expect, it, vi } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";

const store = new Map<string, string>();
vi.mock("next/headers", () => ({
  cookies: async () => ({
    get: (name: string) => (store.has(name) ? { name, value: store.get(name) } : undefined),
    set: (name: string, value: string) => void store.set(name, value),
    delete: (name: string) => void store.delete(name),
  }),
}));

describe.skipIf(!testDatabaseUrl)("sessions", () => {
  it("round-trips a signed-in user and stores only a hash", async () => {
    const handle = await withDatabase();
    store.clear();
    const { createSession, currentUser, SESSION_COOKIE } = await import("./session");
    const { sessions } = await import("@/db/schema");

    const [user] = await handle.insert(users).values({ githubId: 7, login: "lucas" }).returning();
    const { token } = await createSession(user.id);

    expect(store.get(SESSION_COOKIE)).toBe(token);
    const rows = await handle.select().from(sessions);
    expect(rows[0].tokenHash).not.toBe(token);
    expect(await currentUser()).toMatchObject({ id: user.id, login: "lucas" });
  });

  it("ignores an expired session", async () => {
    const handle = await withDatabase();
    store.clear();
    const { createSession, currentUser } = await import("./session");
    const { sessions } = await import("@/db/schema");
    const [user] = await handle.insert(users).values({ githubId: 8, login: "x" }).returning();

    await createSession(user.id);
    await handle.update(sessions).set({ expiresAt: new Date(Date.now() - 1000) });

    expect(await currentUser()).toBeNull();
  });
});
