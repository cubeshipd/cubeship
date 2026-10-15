import { catalogResponse } from "@/lib/catalog-api";

export async function GET(
  request: Request,
  context: RouteContext<"/api/v2/templates/[[...path]]">,
) {
  return catalogResponse(request, ["templates", ...((await context.params).path ?? [])], 2);
}
