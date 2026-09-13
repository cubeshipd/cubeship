import { cache } from "react";
import { catalogUrl } from "@/lib/env";
import type { NormalizedManifest } from "@/lib/manifest";

// The site's only source of templates: hosted/catalog's API, reached on
// the instance's internal network. The site holds no database and no
// bucket; see docs/design/hosted/templates.md.

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
  release: ReleaseRef;
};

export type TemplateDetail = TemplateSummary & {
  readme: string;
  source: string;
  source_url: string;
  manifest: NormalizedManifest | null;
};

export type ReleaseRecord = ReleaseRef & {
  status: "accepted" | "rejected";
  problems: { severity: string; code: string; message: string }[];
};

// A pass writes at most every five minutes, so a minute of cache costs
// nothing an author would notice and spares the catalog a read per view.
const REVALIDATE = 60;

async function read<T>(path: string): Promise<T | null> {
  const response = await fetch(`${catalogUrl()}/v1${path}`, {
    next: { revalidate: REVALIDATE },
    signal: AbortSignal.timeout(5_000),
  });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`the catalog answered ${response.status} for ${path}`);
  return (await response.json()) as T;
}

function templatePath(owner: string, repo: string): string {
  return `/templates/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}`;
}

export async function listTemplates(options: {
  q?: string;
  tag?: string;
  sort?: Sort;
  cursor?: string;
  limit?: number;
}): Promise<{ templates: TemplateSummary[]; next_cursor: string | null }> {
  const params = new URLSearchParams();
  if (options.q) params.set("q", options.q);
  if (options.tag) params.set("tag", options.tag);
  if (options.sort && options.sort !== "recent") params.set("sort", options.sort);
  if (options.cursor) params.set("cursor", options.cursor);
  if (options.limit) params.set("limit", String(options.limit));
  const page = await read<{ templates: TemplateSummary[]; next_cursor: string | null }>(
    params.size > 0 ? `/templates?${params}` : "/templates",
  );
  return page ?? { templates: [], next_cursor: null };
}

export async function distinctTags(): Promise<string[]> {
  return (await read<{ tags: string[] }>("/tags"))?.tags ?? [];
}

// One read per request: a page's metadata and its body both ask.
export const templateByPath = cache((owner: string, repo: string) =>
  read<TemplateDetail>(templatePath(owner, repo)),
);

export async function releasesOf(owner: string, repo: string): Promise<ReleaseRecord[]> {
  return (
    (await read<{ releases: ReleaseRecord[] }>(`${templatePath(owner, repo)}/releases`))
      ?.releases ?? []
  );
}

// Every listed template, for the sitemap: a sitemap that stops at a page
// is not one. Bounded, so a catalog that grew past it still answers.
export async function allTemplates(): Promise<TemplateSummary[]> {
  const all: TemplateSummary[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < 50; page++) {
    const { templates, next_cursor } = await listTemplates({ cursor, limit: 48 });
    all.push(...templates);
    if (!next_cursor) break;
    cursor = next_cursor;
  }
  return all;
}
