import { and, eq, sql } from "drizzle-orm";
import { db } from "@/db/client";
import { likes, templates } from "@/db/schema";
import type { SessionUser } from "@/lib/auth/session";
import { HttpError } from "@/lib/http";

// The count is denormalized so the catalog can sort on it without a join
// per row, and it is written in the same transaction as the row.
export async function setLike(
  user: SessionUser,
  slug: string,
  liked: boolean,
): Promise<{ likesCount: number; liked: boolean }> {
  return db().transaction(async (tx) => {
    const [template] = await tx.select().from(templates).where(eq(templates.slug, slug)).limit(1);
    if (template?.status !== "published") {
      throw new HttpError(404, "not_found", "no template of that name");
    }

    const changed = liked
      ? await tx
          .insert(likes)
          .values({ templateId: template.id, userId: user.id })
          .onConflictDoNothing()
          .returning()
      : await tx
          .delete(likes)
          .where(and(eq(likes.templateId, template.id), eq(likes.userId, user.id)))
          .returning();

    if (changed.length === 0) return { likesCount: template.likesCount, liked };

    const [updated] = await tx
      .update(templates)
      .set({ likesCount: sql`${templates.likesCount} + ${liked ? 1 : -1}` })
      .where(eq(templates.id, template.id))
      .returning({ likesCount: templates.likesCount });

    return { likesCount: updated.likesCount, liked };
  });
}

export async function hasLiked(userId: number, templateId: number): Promise<boolean> {
  const [row] = await db()
    .select({ templateId: likes.templateId })
    .from(likes)
    .where(and(eq(likes.templateId, templateId), eq(likes.userId, userId)))
    .limit(1);
  return row !== undefined;
}
