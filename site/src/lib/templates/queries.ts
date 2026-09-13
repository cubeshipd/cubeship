import { and, desc, eq, inArray, sql } from "drizzle-orm";
import { cache } from "react";
import { db } from "@/db/client";
import { templates, templateVersions, users } from "@/db/schema";
import type { SessionUser } from "@/lib/auth/session";

// No cap, unlike listTemplates: a sitemap has to name every published
// template or it is not one, and the catalog is nowhere near the size
// where that becomes a problem.
export async function allPublishedTemplates(): Promise<{ slug: string; updatedAt: Date }[]> {
  return db()
    .select({ slug: templates.slug, updatedAt: templates.updatedAt })
    .from(templates)
    .where(eq(templates.status, "published"));
}

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

async function readTemplateWithAuthor(slug: string) {
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
  status: string;
  author: { login: string; avatarUrl: string | null };
  updatedAt: Date;
  creates: { apps: number; databases: number; engines: string[] };
};

// Shared by listTemplates and templatesByAuthor: the manifest column is
// jsonb, so the count-and-engines summary has to be computed here rather
// than read off a static type, and both callers want it the same way.
function toCatalogRow(row: {
  slug: string;
  name: string;
  summary: string;
  imageKey: string | null;
  tags: string[];
  likesCount: number;
  status: string;
  manifest: unknown;
  login: string;
  avatarUrl: string | null;
  updatedAt: Date;
}): CatalogRow {
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
    status: row.status,
    author: { login: row.login, avatarUrl: row.avatarUrl },
    updatedAt: row.updatedAt,
    creates: {
      apps: manifest.apps.length,
      databases: manifest.databases.length,
      engines: [...new Set(manifest.databases.map((database) => database.engine))],
    },
  };
}

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
      status: templates.status,
      createdAt: templates.createdAt,
      manifest: templateVersions.manifest,
      login: users.login,
      avatarUrl: users.avatarUrl,
      updatedAt: templates.updatedAt,
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

  return { rows: page.map(toCatalogRow), nextCursor };
}

// No cursor: an author's own templates are few enough that one page is
// the only page. Ordered by the most recently touched, which is what an
// author cares about on their own listing in a way a stranger browsing
// the catalog does not.
export async function templatesByAuthor(
  authorId: number,
  statuses: string[],
): Promise<CatalogRow[]> {
  const rows = await db()
    .select({
      slug: templates.slug,
      name: templates.name,
      summary: templates.summary,
      imageKey: templates.imageKey,
      tags: templates.tags,
      likesCount: templates.likesCount,
      status: templates.status,
      manifest: templateVersions.manifest,
      login: users.login,
      avatarUrl: users.avatarUrl,
      updatedAt: templates.updatedAt,
    })
    .from(templates)
    .innerJoin(users, eq(users.id, templates.authorId))
    .leftJoin(templateVersions, eq(templateVersions.id, templates.currentVersionId))
    .where(and(eq(templates.authorId, authorId), inArray(templates.status, statuses)))
    .orderBy(desc(templates.updatedAt));

  return rows.map(toCatalogRow);
}

// One read per request: a page's metadata and its body both ask.
export const templateWithAuthor = cache(readTemplateWithAuthor);
