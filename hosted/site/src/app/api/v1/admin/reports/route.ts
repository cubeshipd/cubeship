import { requireAdmin } from "@/lib/auth/guard";
import { json, route } from "@/lib/http";
import { openReports } from "@/lib/templates/social";

export const dynamic = "force-dynamic";

export const GET = route(async () => {
  const admin = await requireAdmin();
  return json(await openReports(admin));
});
