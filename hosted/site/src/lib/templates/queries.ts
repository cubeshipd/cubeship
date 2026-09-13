import { and, desc, eq, isNull, sql } from "drizzle-orm";
import { cache } from "react";
import { db } from "@/db/client";
import { releases, repositories } from "@/db/schema";
import type { NormalizedManifest } from "@/lib/manifest";
import { displayName, tagsOf } from "./names";
import { Topic } from "./topic";

// A template is listed when its repository is not hidden and at least
// one release was accepted; what the catalog shows is that release.
const listed = and(isNull(repositories.hidden), sql`${repositories.latestReleaseId} is not null`);

export type CatalogRow = {
  owner: string;
  name: string;
  title: string;
  description: string;
  stars: number;
  tags: string[];
  avatarUrl: string;
  iconKey: string | null;
  publishedAt: Date;
};

const catalogColumns = {
  id: repositories.id,
  owner: repositories.owner,
  name: repositories.name,
  description: repositories.description,
  stars: repositories.stars,
  topics: repositories.topics,
  avatarUrl: repositories.ownerAvatarUrl,
  iconKey: releases.iconKey,
  publishedAt: releases.publishedAt,
};

function toRow(row: {
  owner: string;
  name: string;
  description: string;
  stars: number;
  topics: string[];
  avatarUrl: string;
  iconKey: string | null;
  publishedAt: Date;
}): CatalogRow {
  return {
    owner: row.owner,
    name: row.name,
    title: displayName(row.name),
    description: row.description,
    stars: row.stars,
    tags: tagsOf(row.topics),
    avatarUrl: row.avatarUrl,
    iconKey: row.iconKey,
    publishedAt: row.publishedAt,
  };
}

// Keyset paging, not offset: the catalog is sorted by something that
// changes between two page loads.
export function encodeCursor(parts: [number, number]): string {
  return Buffer.from(parts.join(":")).toString("base64url");
}

export function decodeCursor(cursor: string): [number, number] | undefined {
  const [first, second] = Buffer.from(cursor, "base64url").toString().split(":").map(Number);
  return Number.isFinite(first) && Number.isFinite(second) ? [first, second] : undefined;
}

export type Sort = "recent" | "stars";

export async function listTemplates(options: {
  q?: string;
  tag?: string;
  sort?: Sort;
  cursor?: string;
  limit?: number;
}): Promise<{ rows: CatalogRow[]; nextCursor: string | null }> {
  const limit = Math.min(Math.max(options.limit ?? 24, 1), 48);
  const sort = options.sort ?? "recent";
  const after = options.cursor ? decodeCursor(options.cursor) : undefined;

  const conditions = [listed];
  if (options.tag) conditions.push(sql`${repositories.topics} @> array[${options.tag}]::text[]`);
  if (options.q) {
    const like = `%${options.q.toLowerCase()}%`;
    conditions.push(
      sql`(lower(${repositories.name}) like ${like} or lower(${repositories.description}) like ${like})`,
    );
  }
  if (after) {
    conditions.push(
      sort === "stars"
        ? sql`(${repositories.stars}, ${repositories.id}) < (${after[0]}, ${after[1]})`
        : sql`(extract(epoch from ${releases.publishedAt})::bigint, ${repositories.id}) < (${after[0]}, ${after[1]})`,
    );
  }

  const rows = await db()
    .select(catalogColumns)
    .from(repositories)
    .innerJoin(releases, eq(releases.id, repositories.latestReleaseId))
    .where(and(...conditions))
    .orderBy(
      sort === "stars" ? desc(repositories.stars) : desc(releases.publishedAt),
      desc(repositories.id),
    )
    .limit(limit + 1);

  const page = rows.slice(0, limit);
  const last = page.at(-1);
  const nextCursor =
    rows.length > limit && last
      ? encodeCursor([
          sort === "stars" ? last.stars : Math.floor(last.publishedAt.getTime() / 1000),
          last.id,
        ])
      : null;

  return { rows: page.map(toRow), nextCursor };
}

// The chips on the catalog: every topic a listed template carries.
export async function distinctTags(): Promise<string[]> {
  const result = await db().execute<{ tag: string }>(
    sql`select distinct unnest(topics) as tag from repositories
        where hidden is null and latest_release_id is not null
        order by tag limit 41`,
  );
  return result.rows.map((row) => row.tag).filter((tag) => tag !== Topic);
}

// No cap: a sitemap has to name every template or it is not one.
export async function allTemplates(): Promise<
  { owner: string; name: string; publishedAt: Date }[]
> {
  return db()
    .select({
      owner: repositories.owner,
      name: repositories.name,
      publishedAt: releases.publishedAt,
    })
    .from(repositories)
    .innerJoin(releases, eq(releases.id, repositories.latestReleaseId))
    .where(listed);
}

async function readTemplate(owner: string, name: string) {
  const [row] = await db()
    .select({ repository: repositories, release: releases })
    .from(repositories)
    .innerJoin(releases, eq(releases.id, repositories.latestReleaseId))
    .where(
      and(
        listed,
        sql`lower(${repositories.owner}) = lower(${owner})`,
        sql`lower(${repositories.name}) = lower(${name})`,
      ),
    )
    // Two repositories can briefly answer to one name across a rename.
    .orderBy(desc(repositories.checkedAt))
    .limit(1);
  if (!row) return undefined;
  return {
    ...row,
    title: displayName(row.repository.name),
    tags: tagsOf(row.repository.topics),
    // jsonb carries no static type of its own; discovery wrote it from
    // product/template.Normalized.
    manifest: row.release.manifest as NormalizedManifest | null,
  };
}

// One read per request: a page's metadata and its body both ask.
export const templateByPath = cache(readTemplate);

export async function acceptedReleases(repositoryId: number) {
  return db()
    .select({
      tag: releases.tag,
      name: releases.name,
      url: releases.url,
      publishedAt: releases.publishedAt,
    })
    .from(releases)
    .where(and(eq(releases.repositoryId, repositoryId), eq(releases.status, "accepted")))
    .orderBy(desc(releases.publishedAt))
    .limit(20);
}
