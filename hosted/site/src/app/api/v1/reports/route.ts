import { z } from "zod";
import { requireUser } from "@/lib/auth/guard";
import { HttpError, json, route } from "@/lib/http";
import { report } from "@/lib/templates/social";

const body = z.object({
  subjectType: z.enum(["template", "comment"]),
  subjectId: z.number().int(),
  reason: z.string(),
  note: z.string().max(500).optional(),
});

export const dynamic = "force-dynamic";

export const POST = route(async (request) => {
  const user = await requireUser();
  const parsed = body.safeParse(await request.json());
  if (!parsed.success) throw new HttpError(422, "invalid_body", parsed.error.issues[0].message);

  await report(user, parsed.data);
  return json({ ok: true }, { status: 201 });
});
