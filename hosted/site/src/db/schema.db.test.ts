import { describe, expect, it } from "vitest";
import { users } from "./schema";
import { testDatabaseUrl, withDatabase } from "./testing";

describe.skipIf(!testDatabaseUrl)("the schema", () => {
  it("migrates and holds a user", async () => {
    const handle = await withDatabase();
    const [user] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();

    expect(user.role).toBe("user");
  });

  it("refuses a second user with the same GitHub id", async () => {
    const handle = await withDatabase();
    await handle.insert(users).values({ githubId: 1, login: "lucas" });

    await expect(handle.insert(users).values({ githubId: 1, login: "other" })).rejects.toThrow();
  });
});
