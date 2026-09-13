import { z } from "zod";
import { requireUser } from "@/lib/auth/guard";
import { HttpError, json, route } from "@/lib/http";
import { deleteComment, editComment } from "@/lib/templates/social";

const body = z.object({ body: z.string().min(1) });

export const dynamic = "force-dynamic";

export const PATCH = route<{ id: string }>(async (request, context) => {
  const user = await requireUser();
  const { id } = (await context?.params) ?? { id: "" };
  const parsed = body.safeParse(await request.json());
  if (!parsed.success) throw new HttpError(422, "invalid_body", parsed.error.issues[0].message);

  return json(await editComment(user, Number(id), parsed.data.body));
});

export const DELETE = route<{ id: string }>(async (_request, context) => {
  const user = await requireUser();
  const { id } = (await context?.params) ?? { id: "" };

  await deleteComment(user, Number(id));
  return json({ ok: true });
});
