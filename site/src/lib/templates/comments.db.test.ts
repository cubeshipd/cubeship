import { describe, expect, it } from "vitest";
import { users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { createDraft, publishVersion } from "./publish";
import { templateBySlug } from "./queries";
import { addComment, deleteComment, editComment, listComments } from "./social";

async function seed() {
  const handle = await withDatabase();
  const [row] = await handle.insert(users).values({ githubId: 1, login: "lucas" }).returning();
  const user = { id: row.id, login: row.login, name: null, avatarUrl: null, role: "user" as const };
  const { slug } = await createDraft(user, { name: "Demo", summary: "a demo template", tags: [] });
  await publishVersion(user, slug, {
    source: 'version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n    tag: "1"\n',
  });
  return { user, slug };
}

describe.skipIf(!testDatabaseUrl)("comments", () => {
  it("keeps a reply under its parent", async () => {
    const { user, slug } = await seed();
    const parent = await addComment(user, slug, { body: "does this need a volume?" });
    await addComment(user, slug, { body: "no, it is stateless", parentId: parent.id });

    const template = await templateBySlug(slug);
    const thread = await listComments(template?.id ?? 0);

    expect(thread).toHaveLength(2);
    expect(thread[1].parentId).toBe(parent.id);
  });

  it("refuses a reply to a reply", async () => {
    const { user, slug } = await seed();
    const parent = await addComment(user, slug, { body: "first" });
    const reply = await addComment(user, slug, { body: "second", parentId: parent.id });

    await expect(
      addComment(user, slug, { body: "third", parentId: reply.id }),
    ).rejects.toMatchObject({ status: 422 });
  });

  it("refuses a parent belonging to another template", async () => {
    const { user, slug } = await seed();
    const other = await createDraft(user, {
      name: "Other",
      summary: "a second template",
      tags: [],
    });
    await publishVersion(user, other.slug, {
      source: 'version: 1\nproject: other\napps:\n  - key: web\n    image: nginx\n    tag: "1"\n',
    });
    const parent = await addComment(user, other.slug, { body: "on the other template" });

    await expect(
      addComment(user, slug, { body: "wrong parent", parentId: parent.id }),
    ).rejects.toMatchObject({ status: 422 });
  });

  it("refuses a body that is too short or too long", async () => {
    const { user, slug } = await seed();

    await expect(addComment(user, slug, { body: "x" })).rejects.toMatchObject({ status: 422 });
    await expect(addComment(user, slug, { body: "x".repeat(2001) })).rejects.toMatchObject({
      status: 422,
    });
  });

  it("blanks a deleted comment but keeps the thread", async () => {
    const { user, slug } = await seed();
    const comment = await addComment(user, slug, { body: "never mind" });
    await deleteComment(user, comment.id);

    const template = await templateBySlug(slug);
    const [only] = await listComments(template?.id ?? 0);

    expect(only.deleted).toBe(true);
    expect(only.body).toBeNull();
  });

  it("lets an admin delete someone else's comment", async () => {
    const { user, slug } = await seed();
    const comment = await addComment(user, slug, { body: "spam" });
    const [adminRow] = await (await import("@/db/client"))
      .db()
      .insert(users)
      .values({
        githubId: 2,
        login: "admin",
        role: "admin",
      })
      .returning();
    const admin = {
      id: adminRow.id,
      login: adminRow.login,
      name: null,
      avatarUrl: null,
      role: "admin" as const,
    };

    await deleteComment(admin, comment.id);

    const template = await templateBySlug(slug);
    const [only] = await listComments(template?.id ?? 0);
    expect(only.deleted).toBe(true);
  });

  it("refuses a delete by someone who is not the author or an admin", async () => {
    const { user, slug } = await seed();
    const comment = await addComment(user, slug, { body: "mine" });
    const [otherRow] = await (await import("@/db/client"))
      .db()
      .insert(users)
      .values({
        githubId: 3,
        login: "someone-else",
      })
      .returning();
    const other = {
      id: otherRow.id,
      login: otherRow.login,
      name: null,
      avatarUrl: null,
      role: "user" as const,
    };

    await expect(deleteComment(other, comment.id)).rejects.toMatchObject({ status: 403 });
  });

  it("lets the author edit inside the window", async () => {
    const { user, slug } = await seed();
    const comment = await addComment(user, slug, { body: "typo" });
    const edited = await editComment(user, comment.id, "fixed");
    expect(edited.body).toBe("fixed");
  });

  it("refuses an edit from someone who is not the author", async () => {
    const { user, slug } = await seed();
    const comment = await addComment(user, slug, { body: "mine" });
    const [otherRow] = await (await import("@/db/client"))
      .db()
      .insert(users)
      .values({
        githubId: 4,
        login: "not-the-author",
      })
      .returning();
    const other = {
      id: otherRow.id,
      login: otherRow.login,
      name: null,
      avatarUrl: null,
      role: "user" as const,
    };

    await expect(editComment(other, comment.id, "hijacked")).rejects.toMatchObject({
      status: 403,
    });
  });

  it("refuses an edit once the window has passed", async () => {
    const { user, slug } = await seed();
    const comment = await addComment(user, slug, { body: "typo" });
    const { comments } = await import("@/db/schema");
    const { eq } = await import("drizzle-orm");
    const { db } = await import("@/db/client");
    await db()
      .update(comments)
      .set({ createdAt: new Date(Date.now() - 16 * 60 * 1000) })
      .where(eq(comments.id, comment.id));

    await expect(editComment(user, comment.id, "too late")).rejects.toMatchObject({
      status: 403,
    });
  });

  it("rate-limits a flood", async () => {
    const { user, slug } = await seed();
    for (let i = 0; i < 10; i++) await addComment(user, slug, { body: `comment ${i}` });

    await expect(addComment(user, slug, { body: "one too many" })).rejects.toMatchObject({
      status: 429,
    });
  });
});
