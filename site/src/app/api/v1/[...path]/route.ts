import { InvalidCatalogQuery, listTemplates, type Sort, templateByPath } from "@/lib/catalog";
import { catalogSnapshotAt } from "@/lib/catalog-snapshot";

const headers = { "Access-Control-Allow-Origin": "*", "Cache-Control": "public, max-age=60" };
function fail(status: number, code: string, message: string) {
  return Response.json(
    { error: { code, message } },
    { status, headers: { ...headers, "Cache-Control": "no-store" } },
  );
}

export async function GET(request: Request, context: RouteContext<"/api/v1/[...path]">) {
  const { path } = await context.params;
  const query = new URL(request.url).searchParams;
  try {
    if (path.length === 1 && path[0] === "templates") {
      if (query.has("limit") && !/^[0-9]+$/.test(query.get("limit") ?? ""))
        return fail(400, "invalid_query", "limit is between 1 and 48");
      const result = await listTemplates({
        q: query.get("q") ?? undefined,
        tag: query.get("tag") ?? undefined,
        sort: (query.get("sort") || "recent") as Sort,
        cursor: query.get("cursor") ?? undefined,
        limit: query.has("limit") ? Number(query.get("limit")) : undefined,
      });
      return Response.json(result, { headers });
    }
    if (path.length === 1 && path[0] === "tags") return Response.json({ tags: [] }, { headers });
    if (
      path[0] === "templates" &&
      (path.length === 3 || (path.length === 4 && path[3] === "manifest"))
    ) {
      if (path.length === 4 && query.get("release"))
        return fail(400, "invalid_query", "template releases are no longer selectable");
      const template = await templateByPath(path[1], path[2]);
      if (!template) return fail(404, "not_found", "no template is listed at that address");
      return Response.json(path.length === 4 ? template.manifest : template, { headers });
    }
    if (path.length === 3 && path[0] === "icons") {
      const snapshot = await catalogSnapshotAt(path[2].replace(/\.png$/, ""));
      const entry = snapshot?.templates.find((t) => t.id === path[1]);
      if (!snapshot || !entry || path[2] !== `${snapshot.commit}.png`)
        return fail(404, "not_found", "no such icon");
      return new Response(new Uint8Array(Buffer.from(entry.icon, "base64")), {
        headers: {
          ...headers,
          "Content-Type": "image/png",
          "Cache-Control": "public, max-age=31536000, immutable",
        },
      });
    }
    return fail(404, "not_found", "no such route");
  } catch (error) {
    if (error instanceof InvalidCatalogQuery) return fail(400, "invalid_query", error.message);
    console.error("catalog API:", error);
    return fail(503, "unavailable", "the template catalog is unavailable right now");
  }
}
