import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { createDraft, publishVersion, updateMetadata } from "./publish";
import { templateBySlug, versionsOf } from "./queries";

const good = `version: 1
project: demo
apps:
  - key: web
    image: nginx
    tag: "1"
    health: /
    port: 80
    limits: { cpu: 1, memory: 512Mi }
    domains: []
`;

async function author() {
  const handle = await withDatabase();
  const [user] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
  return { id: user.id, login: user.login, name: null, avatarUrl: null, role: "user" as const };
}

describe.skipIf(!testDatabaseUrl)("publishing", () => {
  it("starts as a draft with no version", async () => {
    const user = await author();
    const { slug } = await createDraft(user, {
      name: "My Stack",
      summary: "s",
      tags: ["analytics"],
    });
    const template = await templateBySlug(slug);

    expect(slug).toBe("my-stack");
    expect(template?.status).toBe("draft");
    expect(template?.currentVersionId).toBeNull();
  });

  it("numbers versions from one and moves the pointer", async () => {
    const user = await author();
    const { slug } = await createDraft(user, { name: "s", summary: "s", tags: [] });

    expect((await publishVersion(user, slug, { source: good })).number).toBe(1);
    expect((await publishVersion(user, slug, { source: good, notes: "again" })).number).toBe(2);

    const template = await templateBySlug(slug);
    const versions = await versionsOf(template?.id ?? 0);
    expect(versions).toHaveLength(2);
    expect(template?.status).toBe("published");
  });

  it("refuses a file with an error, with the diagnostics", async () => {
    const user = await author();
    const { slug } = await createDraft(user, { name: "s", summary: "s", tags: [] });

    await expect(
      publishVersion(user, slug, { source: "version: 1\napps: []\n" }),
    ).rejects.toMatchObject({
      status: 422,
      code: "invalid_template",
    });
  });

  it("refuses somebody else's template", async () => {
    const user = await author();
    const { slug } = await createDraft(user, { name: "s", summary: "s", tags: [] });
    const other = { ...user, id: user.id + 1 };

    await expect(updateMetadata(other, slug, { name: "mine now" })).rejects.toMatchObject({
      status: 404,
    });
  });

  it("gives a second template of the same name its own slug", async () => {
    const user = await author();
    await createDraft(user, { name: "Stack", summary: "s", tags: [] });

    expect((await createDraft(user, { name: "Stack", summary: "s", tags: [] })).slug).toBe(
      "stack-2",
    );
  });
});
