import { catalogResponse } from "@/lib/catalog-api";

export async function GET(request: Request, context: RouteContext<"/api/v1/[...path]">) {
  return catalogResponse(request, (await context.params).path, 1);
}
