import { catalogResponse } from "@/lib/catalog-api";

export async function GET(request: Request, context: RouteContext<"/api/v2/icons/[...path]">) {
  return catalogResponse(request, ["icons", ...(await context.params).path], 2);
}
