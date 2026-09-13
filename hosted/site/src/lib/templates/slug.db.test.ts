import { describe, expect, it } from "vitest";
import { templates, users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { freeSlug } from "./slug";

describe.skipIf(!testDatabaseUrl)("freeSlug", () => {
  it("slugifies a name", async () => {
    await withDatabase();
    expect(await freeSlug("My Cool Stack!")).toBe("my-cool-stack");
  });

  it("finds the next free slug on a collision", async () => {
    const handle = await withDatabase();
    const [user] = await handle.insert(users).values({ githubId: 1, login: "a" }).returning();
    await handle
      .insert(templates)
      .values({ slug: "stack", authorId: user.id, name: "Stack", summary: "s" });

    expect(await freeSlug("Stack")).toBe("stack-2");
  });

  it("falls back to a generic base for a name with nothing left after slugifying", async () => {
    await withDatabase();
    expect(await freeSlug("!!!")).toBe("template");
  });
});
