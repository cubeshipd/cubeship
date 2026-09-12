import { HttpError, json, route } from "@/lib/http";
import { currentVersion, templateBySlug, versionOf } from "@/lib/templates/queries";

export const dynamic = "force-dynamic";

export const GET = route<{ slug: string }>(async (request, context) => {
  const { slug } = (await context?.params) ?? { slug: "" };
  const template = await templateBySlug(slug);
  if (!template || template.status === "removed" || template.status === "draft") {
    throw new HttpError(404, "not_found", "no template of that name");
  }

  const pinned = Number(new URL(request.url).searchParams.get("version"));
  const version = pinned ? await versionOf(template.id, pinned) : await currentVersion(template.id);
  if (!version) throw new HttpError(404, "not_found", "no version of that number");

  return json(version.manifest, {
    headers: { "access-control-allow-origin": "*", "cache-control": "public, max-age=60" },
  });
});
