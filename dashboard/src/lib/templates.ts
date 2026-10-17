import { api, type TemplatePage, type TemplateSummary } from "@/lib/api";

export async function fetchTemplates(search: string, category: string, signal: AbortSignal) {
  const query = new URLSearchParams();
  if (search) query.set("q", search);
  if (category !== "all") query.set("tag", category);
  query.set("limit", "48");
  const all = new Map<string, TemplateSummary>();
  const visited = new Set<string>();
  for (;;) {
    const result = await api.get<TemplatePage>(`/templates?${query}`, { signal });
    for (const template of result.templates) {
      const key = `${template.owner}/${template.name}`;
      if (!all.has(key)) all.set(key, template);
    }
    if (!result.next_cursor)
      return [...all.values()].sort((a, b) => a.title.localeCompare(b.title));
    if (visited.has(result.next_cursor)) {
      throw new Error("The catalog could not finish loading. Please try again.");
    }
    visited.add(result.next_cursor);
    query.set("cursor", result.next_cursor);
  }
}
