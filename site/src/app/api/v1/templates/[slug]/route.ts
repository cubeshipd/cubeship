import { z } from "zod";
import { requireUser } from "@/lib/auth/guard";
import { currentUser } from "@/lib/auth/session";
import { HttpError, json, route } from "@/lib/http";
import { updateMetadata } from "@/lib/templates/publish";
import { currentVersion, templateWithAuthor, versionsOf, visibleTo } from "@/lib/templates/queries";

const patchBody = z.object({
  name: z.string().min(3).max(60).optional(),
  summary: z.string().min(10).max(160).optional(),
  tags: z.array(z.string().min(2).max(20)).max(5).optional(),
  imageKey: z.string().optional(),
  status: z.enum(["draft", "published", "unlisted"]).optional(),
});

export const dynamic = "force-dynamic";

export const GET = route<{ slug: string }>(async (_request, context) => {
  const { slug } = (await context?.params) ?? { slug: "" };
  const [row, viewer] = await Promise.all([templateWithAuthor(slug), currentUser()]);
  if (!row || !visibleTo(row.template, viewer)) {
    throw new HttpError(404, "not_found", "no template of that name");
  }

  const [version, versions] = await Promise.all([
    currentVersion(row.template.id),
    versionsOf(row.template.id),
  ]);

  return json(
    {
      template: row.template,
      author: row.author,
      version: version
        ? {
            number: version.number,
            manifest: version.manifest,
            source: version.source,
            notes: version.notes,
          }
        : null,
      versions: versions.map((v) => v.number),
    },
    { headers: { "access-control-allow-origin": "*" } },
  );
});

export const PATCH = route<{ slug: string }>(async (request, context) => {
  const user = await requireUser();
  const { slug } = (await context?.params) ?? { slug: "" };
  const parsed = patchBody.safeParse(await request.json());
  if (!parsed.success) throw new HttpError(422, "invalid_body", parsed.error.issues[0].message);

  await updateMetadata(user, slug, parsed.data);
  return json({ ok: true });
});

// Soft: the slug stays taken forever, and a like or comment made against
// it keeps pointing at something real.
export const DELETE = route<{ slug: string }>(async (_request, context) => {
  const user = await requireUser();
  const { slug } = (await context?.params) ?? { slug: "" };
  await updateMetadata(user, slug, { status: "removed" });
  return json({ ok: true });
});
