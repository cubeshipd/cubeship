import { requireUser } from "@/lib/auth/guard";
import { json, route } from "@/lib/http";
import { setLike } from "@/lib/templates/social";

export const PUT = route<{ slug: string }>(async (_request, context) => {
  const user = await requireUser();
  const { slug } = (await context?.params) ?? { slug: "" };
  return json(await setLike(user, slug, true));
});

export const DELETE = route<{ slug: string }>(async (_request, context) => {
  const user = await requireUser();
  const { slug } = (await context?.params) ?? { slug: "" };
  return json(await setLike(user, slug, false));
});
