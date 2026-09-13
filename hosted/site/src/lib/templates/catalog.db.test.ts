import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { createDraft, publishVersion } from "./publish";
import { listTemplates } from "./queries";

const source = `version: 1
project: demo
databases:
  - key: db
    engine: postgres
    version: "18"
apps:
  - key: web
    image: nginx
    tag: "1"
    health: /
    port: 80
    limits: { cpu: 1, memory: 512Mi }
`;

describe.skipIf(!testDatabaseUrl)("listTemplates", () => {
  it("lists what is published, newest first, with what it creates", async () => {
    const handle = await withDatabase();
    const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
    const user = {
      id: row.id,
      login: row.login,
      name: null,
      avatarUrl: null,
      role: "user" as const,
    };

    const first = await createDraft(user, {
      name: "Alpha",
      summary: "the first one",
      tags: ["analytics"],
    });
    await publishVersion(user, first.slug, { source });
    await createDraft(user, { name: "Beta", summary: "still a draft", tags: [] });

    const { rows } = await listTemplates({});

    expect(rows.map((r) => r.slug)).toEqual(["alpha"]);
    expect(rows[0].creates).toEqual({ apps: 1, databases: 1, engines: ["postgres"] });
    expect(rows[0].author.login).toBe("lucas");
  });

  it("filters by tag and by text, and pages", async () => {
    const handle = await withDatabase();
    const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
    const user = {
      id: row.id,
      login: row.login,
      name: null,
      avatarUrl: null,
      role: "user" as const,
    };

    for (const name of ["One", "Two", "Three"]) {
      const { slug } = await createDraft(user, { name, summary: `${name} summary`, tags: ["web"] });
      await publishVersion(user, slug, { source });
    }

    expect((await listTemplates({ tag: "web" })).rows).toHaveLength(3);
    expect((await listTemplates({ tag: "none" })).rows).toHaveLength(0);
    expect((await listTemplates({ q: "two" })).rows.map((r) => r.slug)).toEqual(["two"]);

    const page = await listTemplates({ limit: 2 });
    expect(page.rows).toHaveLength(2);
    expect(page.nextCursor).not.toBeNull();
    expect((await listTemplates({ limit: 2, cursor: page.nextCursor ?? "" })).rows).toHaveLength(1);
  });
});
