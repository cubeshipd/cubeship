import { catalogResponse } from "@/lib/catalog-api";

export async function GET(request: Request) {
  return catalogResponse(request, ["tags"], 2);
}
