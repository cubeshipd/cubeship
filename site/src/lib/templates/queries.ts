import { and, desc, eq, sql } from "drizzle-orm";
import { db } from "@/db/client";
import { templates, templateVersions, users } from "@/db/schema";
import type { SessionUser } from "@/lib/auth/session";

// The chips on the catalog: every tag in use by something published,
// not a fixed vocabulary, since authors coin their own.
export async function distinctTags(): Promise<string[]> {
  const result = await db().execute<{ tag: string }>(
    sql`select distinct unnest(tags) as tag from templates where status = 'published' order by tag limit 40`,
  );
  return result.rows.map((row) => row.tag);
}

export async function templateBySlug(slug: string) {
  const [row] = await db().select().from(templates).where(eq(templates.slug, slug)).limit(1);
  return row;
}

// A draft is visible to its author and to an admin, and to nobody
// else; a removed template is visible to nobody. Unlisted is left
// out of that check on purpose — the whole point of unlisted is that
// the direct link still works.
export function visibleTo(
  template: { status: string; authorId: number },
  viewer: SessionUser | null,
): boolean {
  if (template.status === "removed") return false;
  if (template.status === "draft") {
    return viewer !== null && (viewer.id === template.authorId || viewer.role === "admin");
  }
  return true;
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

export type CatalogRow = {
  slug: string;
  name: string;
  summary: string;
  imageKey: string | null;
  tags: string[];
  likesCount: number;
  author: { login: string; avatarUrl: string | null };
  creates: { apps: number; databases: number; engines: string[] };
};

// Keyset paging, not offset: the catalog is sorted by something that
// changes, and an offset page would repeat or skip rows as it does.
export function encodeCursor(parts: [number, number]): string {
  return Buffer.from(parts.join(":")).toString("base64url");
}

export function decodeCursor(cursor: string): [number, number] | undefined {
  const [first, second] = Buffer.from(cursor, "base64url").toString().split(":").map(Number);
  return Number.isFinite(first) && Number.isFinite(second) ? [first, second] : undefined;
}

export async function listTemplates(options: {
  q?: string;
  tag?: string;
  sort?: "recent" | "likes";
  cursor?: string;
  limit?: number;
}): Promise<{ rows: CatalogRow[]; nextCursor: string | null }> {
  const limit = Math.min(Math.max(options.limit ?? 24, 1), 48);
  const sort = options.sort ?? "recent";
  const after = options.cursor ? decodeCursor(options.cursor) : undefined;

  const conditions = [eq(templates.status, "published")];
  if (options.tag) conditions.push(sql`${templates.tags} @> array[${options.tag}]::text[]`);
  if (options.q) {
    const like = `%${options.q.toLowerCase()}%`;
    conditions.push(
      sql`(lower(${templates.name}) like ${like} or lower(${templates.summary}) like ${like})`,
    );
  }
  if (after) {
    conditions.push(
      sort === "likes"
        ? sql`(${templates.likesCount}, ${templates.id}) < (${after[0]}, ${after[1]})`
        : sql`(extract(epoch from ${templates.createdAt})::bigint, ${templates.id}) < (${after[0]}, ${after[1]})`,
    );
  }

  const rows = await db()
    .select({
      id: templates.id,
      slug: templates.slug,
      name: templates.name,
      summary: templates.summary,
      imageKey: templates.imageKey,
      tags: templates.tags,
      likesCount: templates.likesCount,
      createdAt: templates.createdAt,
      manifest: templateVersions.manifest,
      login: users.login,
      avatarUrl: users.avatarUrl,
    })
    .from(templates)
    .innerJoin(users, eq(users.id, templates.authorId))
    .leftJoin(templateVersions, eq(templateVersions.id, templates.currentVersionId))
    .where(and(...conditions))
    .orderBy(
      sort === "likes" ? desc(templates.likesCount) : desc(templates.createdAt),
      desc(templates.id),
    )
    .limit(limit + 1);

  const page = rows.slice(0, limit);
  const last = page.at(-1);
  const nextCursor =
    rows.length > limit && last
      ? encodeCursor([
          sort === "likes" ? last.likesCount : Math.floor(last.createdAt.getTime() / 1000),
          last.id,
        ])
      : null;

  return {
    rows: page.map((row) => {
      const manifest = (row.manifest ?? { apps: [], databases: [] }) as {
        apps: unknown[];
        databases: { engine: string }[];
      };
      return {
        slug: row.slug,
        name: row.name,
        summary: row.summary,
        imageKey: row.imageKey,
        tags: row.tags,
        likesCount: row.likesCount,
        author: { login: row.login, avatarUrl: row.avatarUrl },
        creates: {
          apps: manifest.apps.length,
          databases: manifest.databases.length,
          engines: [...new Set(manifest.databases.map((database) => database.engine))],
        },
      };
    }),
    nextCursor,
  };
}
