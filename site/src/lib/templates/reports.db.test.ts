import { eq } from "drizzle-orm";
import { describe, expect, it } from "vitest";
import { db } from "@/db/client";
import { sessions, templates, users } from "@/db/schema";
import { testDatabaseUrl, withDatabase } from "@/db/testing";
import { createDraft, publishVersion } from "./publish";
import { templateBySlug } from "./queries";
import { blockUser, openReports, report, resolveReport, setTemplateStatus } from "./social";

async function seed() {
  await withDatabase();
  const [authorRow] = await db().insert(users).values({ githubId: 1, login: "author" }).returning();
  const author = {
    id: authorRow.id,
    login: authorRow.login,
    name: null,
    avatarUrl: null,
    role: "user" as const,
  };
  const [adminRow] = await db()
    .insert(users)
    .values({ githubId: 2, login: "admin", role: "admin" })
    .returning();
  const admin = {
    id: adminRow.id,
    login: adminRow.login,
    name: null,
    avatarUrl: null,
    role: "admin" as const,
  };
  const [reporterRow] = await db()
    .insert(users)
    .values({ githubId: 3, login: "reporter" })
    .returning();
  const reporter = {
    id: reporterRow.id,
    login: reporterRow.login,
    name: null,
    avatarUrl: null,
    role: "user" as const,
  };

  const { slug } = await createDraft(author, {
    name: "Demo",
    summary: "a demo template",
    tags: [],
  });
  await publishVersion(author, slug, {
    source: 'version: 1\nproject: demo\napps:\n  - key: web\n    image: nginx\n    tag: "1"\n',
  });
  const template = await templateBySlug(slug);
  if (!template) throw new Error("seed template missing");

  return { author, admin, reporter, slug, template };
}

describe.skipIf(!testDatabaseUrl)("reports", () => {
  it("accepts only the four known reasons", async () => {
    const { reporter, template } = await seed();

    await expect(
      report(reporter, { subjectType: "template", subjectId: template.id, reason: "annoying" }),
    ).rejects.toMatchObject({ status: 422 });

    await expect(
      report(reporter, { subjectType: "template", subjectId: template.id, reason: "spam" }),
    ).resolves.toBeUndefined();
  });

  it("refuses a second report of the same open subject", async () => {
    const { reporter, template } = await seed();
    await report(reporter, { subjectType: "template", subjectId: template.id, reason: "spam" });

    await expect(
      report(reporter, { subjectType: "template", subjectId: template.id, reason: "abuse" }),
    ).rejects.toMatchObject({ status: 409 });
  });

  it("allows reporting again once the open report is resolved", async () => {
    const { admin, reporter, template } = await seed();
    await report(reporter, { subjectType: "template", subjectId: template.id, reason: "spam" });
    const [open] = await openReports(admin);
    await resolveReport(admin, open.id, "no action needed");

    await expect(
      report(reporter, { subjectType: "template", subjectId: template.id, reason: "abuse" }),
    ).resolves.toBeUndefined();
  });

  it("keeps the queue closed to anyone who is not an admin", async () => {
    const { author, reporter, template } = await seed();
    await report(reporter, { subjectType: "template", subjectId: template.id, reason: "spam" });

    await expect(openReports(author)).rejects.toMatchObject({ status: 403 });
  });

  it("lets an admin read the open queue", async () => {
    const { admin, reporter, template } = await seed();
    await report(reporter, { subjectType: "template", subjectId: template.id, reason: "malware" });

    const rows = await openReports(admin);
    expect(rows).toHaveLength(1);
    expect(rows[0].reason).toBe("malware");
    expect(rows[0].reporter.login).toBe("reporter");
  });

  it("refuses resolveReport from anyone who is not an admin", async () => {
    const { author, admin, reporter, template } = await seed();
    await report(reporter, { subjectType: "template", subjectId: template.id, reason: "spam" });
    const [open] = await openReports(admin);

    await expect(resolveReport(author, open.id, "ignored")).rejects.toMatchObject({ status: 403 });
  });

  it("refuses setTemplateStatus from anyone who is not an admin", async () => {
    const { author, slug } = await seed();

    await expect(setTemplateStatus(author, slug, "unlisted")).rejects.toMatchObject({
      status: 403,
    });
  });

  it("lets an admin unlist a reported template", async () => {
    const { admin, slug } = await seed();

    await setTemplateStatus(admin, slug, "unlisted");

    const template = await templateBySlug(slug);
    expect(template?.status).toBe("unlisted");
  });

  it("refuses blockUser from anyone who is not an admin", async () => {
    const { author } = await seed();

    await expect(blockUser(author, "author", true)).rejects.toMatchObject({ status: 403 });
  });

  it("blocking a user deletes their sessions", async () => {
    const { admin, author } = await seed();
    await db()
      .insert(sessions)
      .values({
        tokenHash: "abc",
        userId: author.id,
        expiresAt: new Date(Date.now() + 60_000),
      });

    await blockUser(admin, author.login, true);

    const rows = await db().select().from(sessions).where(eq(sessions.userId, author.id));
    expect(rows).toHaveLength(0);
  });

  it("blocking a user sets each of their templates to unlisted", async () => {
    const { admin, author } = await seed();

    await blockUser(admin, author.login, true);

    const rows = await db().select().from(templates).where(eq(templates.authorId, author.id));
    expect(rows.every((row) => row.status === "unlisted")).toBe(true);
  });
});
