import { z } from "zod";
import { requireUser } from "@/lib/auth/guard";
import { HttpError, json, route } from "@/lib/http";
import { publishVersion } from "@/lib/templates/publish";

const body = z.object({
  source: z.string().min(1),
  notes: z.string().max(500).optional(),
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
