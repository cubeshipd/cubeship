import { z } from "zod";
import { requireUser } from "@/lib/auth/guard";
import { currentUser } from "@/lib/auth/session";
import { HttpError, json, route } from "@/lib/http";
import { templateBySlug, visibleTo } from "@/lib/templates/queries";
import { addComment, listComments } from "@/lib/templates/social";

const body = z.object({
  body: z.string().min(1),
  parentId: z.number().int().optional(),
});

export const dynamic = "force-dynamic";

export const GET = route<{ slug: string }>(async (_request, context) => {
  const { slug } = (await context?.params) ?? { slug: "" };
  const [template, viewer] = await Promise.all([templateBySlug(slug), currentUser()]);
  if (!template || !visibleTo(template, viewer)) {
    throw new HttpError(404, "not_found", "no template of that name");
  }

  return json(await listComments(template.id), {
    headers: { "access-control-allow-origin": "*" },
  });
});

export const POST = route<{ slug: string }>(async (request, context) => {
  const user = await requireUser();
  const { slug } = (await context?.params) ?? { slug: "" };
  const parsed = body.safeParse(await request.json());
  if (!parsed.success) throw new HttpError(422, "invalid_body", parsed.error.issues[0].message);

  return json(await addComment(user, slug, parsed.data), { status: 201 });
});
