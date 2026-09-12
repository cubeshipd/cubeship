import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { createDraft, publishVersion } from "./publish";
import { setLike } from "./social";

describe.skipIf(!testDatabaseUrl)("setLike", () => {
  it("is idempotent in both directions and keeps the count", async () => {
    const handle = await withDatabase();
    const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
    const user = {
      id: row.id,
      login: row.login,
      name: null,
      avatarUrl: null,
      role: "user" as const,
    };
    const { slug } = await createDraft(user, {
      name: "Demo",
      summary: "a demo template",
      tags: [],
    });
    await publishVersion(user, slug, {
      source: 'version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n    tag: "1"\n',
    });

    expect(await setLike(user, slug, true)).toEqual({ likesCount: 1, liked: true });
    expect(await setLike(user, slug, true)).toEqual({ likesCount: 1, liked: true });
    expect(await setLike(user, slug, false)).toEqual({ likesCount: 0, liked: false });
    expect(await setLike(user, slug, false)).toEqual({ likesCount: 0, liked: false });
  });
});
