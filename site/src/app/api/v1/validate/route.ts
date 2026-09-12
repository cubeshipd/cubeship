import { readText, route } from "@/lib/http";
import { validateTemplate } from "@/lib/template";

export const POST = route(async (request) => {
  const source = await readText(request);
  const result = validateTemplate(source);

  return Response.json(result, {
    status: result.ok ? 200 : 422,
    headers: { "access-control-allow-origin": "*" },
  });
});
