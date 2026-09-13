import { z } from "zod";
import { requireUser } from "@/lib/auth/guard";
import { HttpError, json, route } from "@/lib/http";
import { createDraft } from "@/lib/templates/publish";
import { listTemplates } from "@/lib/templates/queries";

const body = z.object({
  name: z.string().min(3).max(60),
  summary: z.string().min(10).max(160),
  tags: z.array(z.string().min(2).max(20)).max(5).default([]),
});

export const dynamic = "force-dynamic";

export const GET = route(async (request) => {
  const params = new URL(request.url).searchParams;
  const sort = params.get("sort") === "likes" ? "likes" : "recent";

  const page = await listTemplates({
    q: params.get("q") ?? undefined,
    tag: params.get("tag") ?? undefined,
    sort,
    cursor: params.get("cursor") ?? undefined,
    limit: Number(params.get("limit")) || undefined,
  });

  return json(page, { headers: { "access-control-allow-origin": "*" } });
});

export const POST = route(async (request) => {
  const user = await requireUser();
  const parsed = body.safeParse(await request.json());
  if (!parsed.success) throw new HttpError(422, "invalid_body", parsed.error.issues[0].message);

  return json(await createDraft(user, parsed.data), { status: 201 });
});
