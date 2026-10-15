import { cache } from "react";
import { catalogSnapshot, type Snapshot, type SnapshotEntry } from "@/lib/catalog-snapshot";
import type { NormalizedManifest } from "@/lib/manifest";

export type Sort = "recent" | "stars";

export type ReleaseRef = {
  tag: string;
  name: string;
  url: string;
  commit: string;
  published_at: string;
};

export type TemplateSummary = {
  owner: string;
  name: string;
  title: string;
  description: string;
  url: string;
  stars: number;
  tags: string[];
  avatar_url: string;
  icon_url: string | null;
  verified: boolean;
  release: ReleaseRef;
};

export type TemplateDetail = TemplateSummary & {
  readme: string;
  source: string;
  source_url: string;
  manifest: NormalizedManifest | null;
};

const publicAPI = "https://cubeship.dev/api/v1";
const repository = "https://github.com/cubeshipd/cubeship-templates";

export function summary(snapshot: Snapshot, entry: SnapshotEntry): TemplateSummary {
  const url = `${repository}/tree/${snapshot.commit}/${entry.slug}`;
  return {
    owner: "cubeshipd",
    name: entry.slug,
    title: entry.name,
    description: entry.description,
    url,
    stars: 0,
    tags: [],
    avatar_url: "https://github.com/cubeshipd.png",
    icon_url: `${publicAPI}/icons/${entry.id}/${snapshot.commit}.png`,
    verified: true,
    // v1 clients still read this field; it names a commit, never a selectable release.
    release: {
      tag: snapshot.commit,
      name: entry.name,
      url,
      commit: snapshot.commit,
      published_at: snapshot.published_at,
    },
  };
}

export class InvalidCatalogQuery extends Error {}

export async function listTemplates(
  options: { q?: string; tag?: string; sort?: Sort; cursor?: string; limit?: number },
  _signal?: AbortSignal,
): Promise<{ templates: TemplateSummary[]; next_cursor: string | null }> {
  const q = (options.q || "").trim().toLowerCase();
  const limit = options.limit ?? 24;
  const sort = options.sort || "recent";
  if (
    q.length > 100 ||
    !Number.isInteger(limit) ||
    limit < 1 ||
    limit > 48 ||
    !["recent", "stars"].includes(sort)
  ) {
    throw new InvalidCatalogQuery("invalid search, sort or limit");
  }
  const snapshot = await catalogSnapshot();
  let after: bigint | undefined;
  if (options.cursor) {
    if (options.cursor.length > 64) throw new InvalidCatalogQuery("invalid cursor");
    const raw = Buffer.from(options.cursor, "base64url").toString();
    if (!/^-?\d+:-?\d+$/.test(raw) || Buffer.from(raw).toString("base64url") !== options.cursor)
      throw new InvalidCatalogQuery("the cursor is not one this API gave out");
    const [key, id] = raw.split(":");
    after = BigInt(id);
    const expected = sort === "stars" ? 0 : Math.floor(Date.parse(snapshot.published_at) / 1000);
    if (key !== String(expected))
      throw new InvalidCatalogQuery("the catalog changed; restart pagination");
  }
  const entries = snapshot.templates
    .filter(
      (t) =>
        !options.tag &&
        (!q || `${t.slug} ${t.name} ${t.description}`.toLowerCase().includes(q)) &&
        (after === undefined || BigInt(t.id) < after),
    )
    .sort((a, b) => (BigInt(a.id) > BigInt(b.id) ? -1 : 1));
  const page = entries.slice(0, limit);
  const key = sort === "stars" ? 0 : Math.floor(Date.parse(snapshot.published_at) / 1000);
  return {
    templates: page.map((t) => summary(snapshot, t)),
    next_cursor:
      entries.length > limit
        ? Buffer.from(`${key}:${page[page.length - 1].id}`).toString("base64url")
        : null,
  };
}

export const templateByPath = cache(
  async (owner: string, repo: string): Promise<TemplateDetail | null> => {
    if (owner.toLowerCase() !== "cubeshipd") return null;
    const snapshot = await catalogSnapshot();
    const entry = snapshot.templates.find((t) => t.slug === repo.toLowerCase());
    if (!entry) return null;
    return {
      ...summary(snapshot, entry),
      readme: entry.readme,
      source: entry.source,
      source_url: `https://raw.githubusercontent.com/cubeshipd/cubeship-templates/${snapshot.commit}/${entry.slug}/template.yaml`,
      manifest: entry.manifest as NormalizedManifest,
    };
  },
);

export async function allTemplates(
  options: { q?: string; tag?: string; sort?: Sort } = {},
): Promise<TemplateSummary[]> {
  const all: TemplateSummary[] = [];
  let cursor: string | undefined;
  do {
    const page = await listTemplates({ ...options, cursor, limit: 48 });
    all.push(...page.templates);
    cursor = page.next_cursor ?? undefined;
  } while (cursor);
  return all.sort((a, b) => a.title.localeCompare(b.title));
}
