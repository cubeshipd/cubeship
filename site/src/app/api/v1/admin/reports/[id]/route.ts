import { z } from "zod";
import { requireAdmin } from "@/lib/auth/guard";
import { HttpError, json, route } from "@/lib/http";
import { deleteComment, resolveReport, setTemplateStatus } from "@/lib/templates/social";

const body = z.object({
  resolution: z.string().min(1).max(500),
  // What to do to the reported thing before the report itself closes;
  // absent means dismiss with no action taken.
  action: z.enum(["unlist", "remove"]).optional(),
  subjectType: z.enum(["template", "comment"]).optional(),
  subjectId: z.number().int().optional(),
  slug: z.string().optional(),
});

export const dynamic = "force-dynamic";

export const PATCH = route<{ id: string }>(async (request, context) => {
  const admin = await requireAdmin();
  const { id } = (await context?.params) ?? { id: "" };
  const parsed = body.safeParse(await request.json());
  if (!parsed.success) throw new HttpError(422, "invalid_body", parsed.error.issues[0].message);
  const { action, subjectType, subjectId, slug, resolution } = parsed.data;

  if (action === "unlist") {
    if (subjectType !== "template" || !slug) {
      throw new HttpError(422, "invalid_body", "unlisting needs a template's slug");
    }
    await setTemplateStatus(admin, slug, "unlisted");
  } else if (action === "remove") {
    if (subjectType === "template") {
      if (!slug) throw new HttpError(422, "invalid_body", "removing a template needs its slug");
      await setTemplateStatus(admin, slug, "removed");
    } else if (subjectType === "comment") {
      if (subjectId === undefined) {
        throw new HttpError(422, "invalid_body", "removing a comment needs its id");
      }
      await deleteComment(admin, subjectId);
    }
  }

  await resolveReport(admin, Number(id), resolution);
  return json({ ok: true });
});
