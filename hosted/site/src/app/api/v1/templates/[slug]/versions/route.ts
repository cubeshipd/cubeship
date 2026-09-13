import { z } from "zod";
import { requireUser } from "@/lib/auth/guard";
import { currentUser } from "@/lib/auth/session";
import { HttpError, json, route } from "@/lib/http";
import { publishVersion } from "@/lib/templates/publish";
import { templateBySlug, versionsOf, visibleTo } from "@/lib/templates/queries";

const body = z.object({
  source: z.string().min(1),
  notes: z.string().max(500).optional(),
});

export const dynamic = "force-dynamic";

export const GET = route<{ slug: string }>(async (_request, context) => {
  const { slug } = (await context?.params) ?? { slug: "" };
  const [template, viewer] = await Promise.all([templateBySlug(slug), currentUser()]);
  if (!template || !visibleTo(template, viewer)) {
    throw new HttpError(404, "not_found", "no template of that name");
  }

  const versions = await versionsOf(template.id);
  return json(
    versions.map((version) => ({
      number: version.number,
      notes: version.notes,
      createdAt: version.createdAt,
    })),
    { headers: { "access-control-allow-origin": "*" } },
  );
});

export const POST = route<{ slug: string }>(async (request, context) => {
  const user = await requireUser();
  const { slug } = (await context?.params) ?? { slug: "" };
  const parsed = body.safeParse(await request.json());
  if (!parsed.success) throw new HttpError(422, "invalid_body", parsed.error.issues[0].message);

  // A diagnostics-carrying HttpError from publishVersion renders through
  // route()'s own fail(); there is nothing else to catch here.
  return json(await publishVersion(user, slug, parsed.data), { status: 201 });
});
