import { and, desc, eq } from "drizzle-orm";
import { db } from "@/db/client";
import { templates, templateVersions, users } from "@/db/schema";

export async function templateBySlug(slug: string) {
  const [row] = await db().select().from(templates).where(eq(templates.slug, slug)).limit(1);
  return row;
}

export async function templateWithAuthor(slug: string) {
  const [row] = await db()
    .select({
      template: templates,
      author: { login: users.login, name: users.name, avatarUrl: users.avatarUrl },
    })
    .from(templates)
    .innerJoin(users, eq(users.id, templates.authorId))
    .where(eq(templates.slug, slug))
    .limit(1);
  return row;
}

export async function versionsOf(templateId: number) {
  return db()
    .select({
      id: templateVersions.id,
      number: templateVersions.number,
      notes: templateVersions.notes,
      createdAt: templateVersions.createdAt,
    })
    .from(templateVersions)
    .where(eq(templateVersions.templateId, templateId))
    .orderBy(desc(templateVersions.number));
}

export async function versionOf(templateId: number, number: number) {
  const [row] = await db()
    .select()
    .from(templateVersions)
    .where(and(eq(templateVersions.templateId, templateId), eq(templateVersions.number, number)))
    .limit(1);
  return row;
}

export async function currentVersion(templateId: number) {
  const [row] = await db()
    .select()
    .from(templateVersions)
    .where(eq(templateVersions.templateId, templateId))
    .orderBy(desc(templateVersions.number))
    .limit(1);
  return row;
}
