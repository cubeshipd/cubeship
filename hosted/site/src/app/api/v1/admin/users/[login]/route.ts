import { z } from "zod";
import { requireAdmin } from "@/lib/auth/guard";
import { HttpError, json, route } from "@/lib/http";
import { blockUser } from "@/lib/templates/social";

const body = z.object({ blocked: z.boolean() });

export const dynamic = "force-dynamic";

export const PATCH = route<{ login: string }>(async (request, context) => {
  const admin = await requireAdmin();
  const { login } = (await context?.params) ?? { login: "" };
  const parsed = body.safeParse(await request.json());
  if (!parsed.success) throw new HttpError(422, "invalid_body", parsed.error.issues[0].message);

  await blockUser(admin, login, parsed.data.blocked);
  return json({ ok: true });
});
