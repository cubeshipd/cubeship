import { and, eq, gte, sql } from "drizzle-orm";
import { db } from "@/db/client";
import { templates, templateVersions } from "@/db/schema";
import type { SessionUser } from "@/lib/auth/session";
import { HttpError } from "@/lib/http";
import { validateTemplate } from "@/lib/template";
import { freeSlug } from "./slug";

const DRAFTS_PER_DAY = 20;

export async function createDraft(
  user: SessionUser,
  input: { name: string; summary: string; tags: string[] },
): Promise<{ slug: string }> {
  const since = new Date(Date.now() - 24 * 60 * 60 * 1000);
  const [{ count }] = await db()
    .select({ count: sql<number>`count(*)::int` })
    .from(templates)
    .where(and(eq(templates.authorId, user.id), gte(templates.createdAt, since)));
  if (count >= DRAFTS_PER_DAY) {
    throw new HttpError(
      429,
      "too_many",
      "that is a lot of templates for one day; try again tomorrow",
    );
  }

  const slug = await freeSlug(input.name);
  await db().insert(templates).values({
    slug,
    authorId: user.id,
    name: input.name,
    summary: input.summary,
    tags: input.tags,
  });

  return { slug };
}

// Ownership is a read that 404s rather than 403s only for a template the
// caller cannot see at all; an author's own template answers normally.
export async function ownedTemplate(user: SessionUser, slug: string) {
  const [row] = await db().select().from(templates).where(eq(templates.slug, slug)).limit(1);
  if (!row || (row.authorId !== user.id && user.role !== "admin")) {
    throw new HttpError(404, "not_found", "no template of that name");
  }
  return row;
}

export async function updateMetadata(
  user: SessionUser,
  slug: string,
  // "removed" is reachable only through DELETE, never through the PATCH
  // body schema, so an author can take a template down but not relist it
  // by hand-crafting the value.
  patch: {
    name?: string;
    summary?: string;
    tags?: string[];
    imageKey?: string;
    status?: "draft" | "published" | "unlisted" | "removed";
  },
): Promise<void> {
  const template = await ownedTemplate(user, slug);
  if (patch.status === "published" && !template.currentVersionId) {
    throw new HttpError(409, "no_version", "publish a version before publishing the template");
  }

  await db()
    .update(templates)
    .set({ ...patch, updatedAt: new Date() })
    .where(eq(templates.id, template.id));
}

export async function publishVersion(
  user: SessionUser,
  slug: string,
  input: { source: string; notes?: string },
): Promise<{ number: number }> {
  const template = await ownedTemplate(user, slug);
  const result = validateTemplate(input.source);
  if (!result.ok || !result.manifest) {
    throw new HttpError(422, "invalid_template", "that template has errors", {
      diagnostics: result.diagnostics,
    });
  }
  const manifest = result.manifest;

  return db().transaction(async (tx) => {
    const [{ highest }] = await tx
      .select({ highest: sql<number>`coalesce(max(number), 0)::int` })
      .from(templateVersions)
      .where(eq(templateVersions.templateId, template.id));
    const number = highest + 1;

    const [version] = await tx
      .insert(templateVersions)
      .values({
        templateId: template.id,
        number,
        manifest,
        source: input.source,
        schemaVersion: manifest.schema_version,
        notes: input.notes ?? null,
      })
      .returning();

    // The first version is also what takes a draft public.
    await tx
      .update(templates)
      .set({
        currentVersionId: version.id,
        status: template.status === "draft" ? "published" : template.status,
        updatedAt: new Date(),
      })
      .where(eq(templates.id, template.id));

    return { number };
  });
}
