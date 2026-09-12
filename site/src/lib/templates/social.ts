import { and, asc, eq, gte, inArray, isNull, sql } from "drizzle-orm";
import { alias } from "drizzle-orm/pg-core";
import { db } from "@/db/client";
import { comments, likes, reports, sessions, templates, users } from "@/db/schema";
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

const COMMENTS_PER_WINDOW = 10;
const RATE_WINDOW_MS = 5 * 60 * 1000;
const EDIT_WINDOW_MS = 15 * 60 * 1000;

export type Comment = {
  id: number;
  parentId: number | null;
  body: string | null;
  deleted: boolean;
  createdAt: Date;
  author: { login: string; avatarUrl: string | null };
};

function assertCommentLength(body: string): void {
  // Plain text, no Markdown: length is the only shape a comment has.
  if (body.length < 2 || body.length > 2000) {
    throw new HttpError(422, "invalid_body", "a comment is between 2 and 2000 characters");
  }
}

export async function addComment(
  user: SessionUser,
  slug: string,
  input: { body: string; parentId?: number },
): Promise<Comment> {
  assertCommentLength(input.body);

  const [template] = await db().select().from(templates).where(eq(templates.slug, slug)).limit(1);
  if (!template) throw new HttpError(404, "not_found", "no template of that name");

  // Counted from the rows themselves rather than a counter table, the
  // same way the daily-draft limit is: nothing to reset, nothing to drift.
  const since = new Date(Date.now() - RATE_WINDOW_MS);
  const [{ recent }] = await db()
    .select({ recent: sql<number>`count(*)::int` })
    .from(comments)
    .where(and(eq(comments.authorId, user.id), gte(comments.createdAt, since)));
  if (recent >= COMMENTS_PER_WINDOW) {
    throw new HttpError(429, "too_many", "slow down and try again in a few minutes");
  }

  let parentId: number | null = null;
  if (input.parentId !== undefined) {
    const [parent] = await db()
      .select()
      .from(comments)
      .where(eq(comments.id, input.parentId))
      .limit(1);
    // There is exactly one level: a reply's parent must be a top-level
    // comment, and it must belong to this template.
    if (!parent || parent.templateId !== template.id || parent.parentId !== null) {
      throw new HttpError(
        422,
        "invalid_parent",
        "a reply's parent must be a top-level comment on the same template",
      );
    }
    parentId = parent.id;
  }

  const [row] = await db()
    .insert(comments)
    .values({ templateId: template.id, authorId: user.id, parentId, body: input.body })
    .returning();

  return {
    id: row.id,
    parentId: row.parentId,
    body: row.body,
    deleted: false,
    createdAt: row.createdAt,
    author: { login: user.login, avatarUrl: user.avatarUrl },
  };
}

export async function listComments(templateId: number): Promise<Comment[]> {
  const rows = await db()
    .select({
      id: comments.id,
      parentId: comments.parentId,
      body: comments.body,
      deletedAt: comments.deletedAt,
      createdAt: comments.createdAt,
      login: users.login,
      avatarUrl: users.avatarUrl,
    })
    .from(comments)
    .innerJoin(users, eq(users.id, comments.authorId))
    .where(eq(comments.templateId, templateId))
    .orderBy(asc(comments.createdAt), asc(comments.id));

  return rows.map((row) => ({
    id: row.id,
    parentId: row.parentId,
    // Deleting blanks the body here, in the read, so the row (and the
    // thread's shape) stays in place.
    body: row.deletedAt ? null : row.body,
    deleted: row.deletedAt !== null,
    createdAt: row.createdAt,
    author: { login: row.login, avatarUrl: row.avatarUrl },
  }));
}

export async function editComment(user: SessionUser, id: number, body: string): Promise<Comment> {
  assertCommentLength(body);

  const [row] = await db().select().from(comments).where(eq(comments.id, id)).limit(1);
  if (!row || row.deletedAt) throw new HttpError(404, "not_found", "no comment of that id");
  if (row.authorId !== user.id) {
    throw new HttpError(403, "forbidden", "only the author may edit this comment");
  }
  if (Date.now() - row.createdAt.getTime() > EDIT_WINDOW_MS) {
    throw new HttpError(403, "edit_expired", "the edit window has passed");
  }

  const [updated] = await db()
    .update(comments)
    .set({ body, updatedAt: new Date() })
    .where(eq(comments.id, id))
    .returning();

  return {
    id: updated.id,
    parentId: updated.parentId,
    body: updated.body,
    deleted: false,
    createdAt: updated.createdAt,
    author: { login: user.login, avatarUrl: user.avatarUrl },
  };
}

export async function deleteComment(user: SessionUser, id: number): Promise<void> {
  const [row] = await db().select().from(comments).where(eq(comments.id, id)).limit(1);
  if (!row) throw new HttpError(404, "not_found", "no comment of that id");
  if (row.authorId !== user.id && user.role !== "admin") {
    throw new HttpError(403, "forbidden", "only the author or an admin may delete this comment");
  }

  await db().update(comments).set({ deletedAt: new Date() }).where(eq(comments.id, id));
}

const REPORT_REASONS = ["spam", "malware", "abuse", "other"] as const;
export type ReportReason = (typeof REPORT_REASONS)[number];

export type ReportRow = {
  id: number;
  subjectType: string;
  subjectId: number;
  reason: string;
  note: string | null;
  createdAt: Date;
  reporter: { login: string };
  // What the link on the admin page points at; slug is set only for a
  // template report, since setTemplateStatus takes a slug, not an id.
  // authorLogin is who blockUser would act on, distinct from the
  // reporter above.
  subject: { label: string; href: string; slug: string | null; authorLogin: string | null };
};

function requireAdminRole(admin: SessionUser): void {
  if (admin.role !== "admin") throw new HttpError(403, "forbidden", "this is for administrators");
}

export async function report(
  user: SessionUser,
  input: { subjectType: "template" | "comment"; subjectId: number; reason: string; note?: string },
): Promise<void> {
  if (!REPORT_REASONS.includes(input.reason as ReportReason)) {
    throw new HttpError(
      422,
      "invalid_reason",
      `reason must be one of ${REPORT_REASONS.join(", ")}`,
    );
  }

  const inserted = await db()
    .insert(reports)
    .values({
      subjectType: input.subjectType,
      subjectId: input.subjectId,
      reporterId: user.id,
      reason: input.reason,
      note: input.note ?? null,
    })
    // Mirrors reports_one_per_person: the same target and the same
    // partial predicate, so Postgres can infer the index and no-op
    // instead of raising 23505 for us to translate by hand.
    .onConflictDoNothing({
      target: [reports.subjectType, reports.subjectId, reports.reporterId],
      where: sql`${reports.resolvedAt} is null`,
    })
    .returning({ id: reports.id });

  if (inserted.length === 0) {
    throw new HttpError(409, "already_reported", "you already reported this, and it is still open");
  }
}

// Trusts its own admin check rather than only the route calling it, the
// same way ownedTemplate answers its own question about ownership.
export async function openReports(admin: SessionUser): Promise<ReportRow[]> {
  requireAdminRole(admin);

  const rows = await db()
    .select({
      id: reports.id,
      subjectType: reports.subjectType,
      subjectId: reports.subjectId,
      reason: reports.reason,
      note: reports.note,
      createdAt: reports.createdAt,
      login: users.login,
    })
    .from(reports)
    .innerJoin(users, eq(users.id, reports.reporterId))
    .where(isNull(reports.resolvedAt))
    .orderBy(asc(reports.createdAt));

  const templateIds = rows
    .filter((row) => row.subjectType === "template")
    .map((row) => row.subjectId);
  const commentIds = rows
    .filter((row) => row.subjectType === "comment")
    .map((row) => row.subjectId);

  // A second alias for users: the reporter is already joined above, and
  // a subject's author is a different row from the same table.
  const author = alias(users, "report_subject_author");

  const templateRows = templateIds.length
    ? await db()
        .select({
          id: templates.id,
          slug: templates.slug,
          name: templates.name,
          authorLogin: author.login,
        })
        .from(templates)
        .innerJoin(author, eq(author.id, templates.authorId))
        .where(inArray(templates.id, templateIds))
    : [];
  const commentRows = commentIds.length
    ? await db()
        .select({
          id: comments.id,
          body: comments.body,
          slug: templates.slug,
          authorLogin: author.login,
        })
        .from(comments)
        .innerJoin(templates, eq(templates.id, comments.templateId))
        .innerJoin(author, eq(author.id, comments.authorId))
        .where(inArray(comments.id, commentIds))
    : [];

  const templateById = new Map(templateRows.map((row) => [row.id, row]));
  const commentById = new Map(commentRows.map((row) => [row.id, row]));

  return rows.map((row) => {
    if (row.subjectType === "template") {
      const found = templateById.get(row.subjectId);
      return {
        id: row.id,
        subjectType: row.subjectType,
        subjectId: row.subjectId,
        reason: row.reason,
        note: row.note,
        createdAt: row.createdAt,
        reporter: { login: row.login },
        subject: found
          ? {
              label: found.name,
              href: `/templates/${found.slug}`,
              slug: found.slug,
              authorLogin: found.authorLogin,
            }
          : { label: "a deleted template", href: "#", slug: null, authorLogin: null },
      };
    }

    const found = commentById.get(row.subjectId);
    return {
      id: row.id,
      subjectType: row.subjectType,
      subjectId: row.subjectId,
      reason: row.reason,
      note: row.note,
      createdAt: row.createdAt,
      reporter: { login: row.login },
      subject: found
        ? {
            label: `comment: “${(found.body ?? "").slice(0, 60)}”`,
            href: `/templates/${found.slug}`,
            slug: null,
            authorLogin: found.authorLogin,
          }
        : { label: "a deleted comment", href: "#", slug: null, authorLogin: null },
    };
  });
}

export async function resolveReport(
  admin: SessionUser,
  id: number,
  resolution: string,
): Promise<void> {
  requireAdminRole(admin);

  await db().update(reports).set({ resolvedAt: new Date(), resolution }).where(eq(reports.id, id));
}

export async function setTemplateStatus(
  admin: SessionUser,
  slug: string,
  status: "draft" | "published" | "unlisted" | "removed",
): Promise<void> {
  requireAdminRole(admin);

  const [template] = await db().select().from(templates).where(eq(templates.slug, slug)).limit(1);
  if (!template) throw new HttpError(404, "not_found", "no template of that name");

  await db()
    .update(templates)
    .set({ status, updatedAt: new Date() })
    .where(eq(templates.id, template.id));
}

export async function blockUser(
  admin: SessionUser,
  login: string,
  blocked: boolean,
): Promise<void> {
  requireAdminRole(admin);

  const [user] = await db().select().from(users).where(eq(users.login, login)).limit(1);
  if (!user) throw new HttpError(404, "not_found", "no user with that login");

  await db().transaction(async (tx) => {
    await tx
      .update(users)
      .set({ blockedAt: blocked ? new Date() : null })
      .where(eq(users.id, user.id));

    // Unblocking only lets them sign in again; it does not relist what
    // blocking took down, which stays a deliberate admin decision.
    if (blocked) {
      await tx.delete(sessions).where(eq(sessions.userId, user.id));
      await tx
        .update(templates)
        .set({ status: "unlisted", updatedAt: new Date() })
        .where(eq(templates.authorId, user.id));
    }
  });
}
