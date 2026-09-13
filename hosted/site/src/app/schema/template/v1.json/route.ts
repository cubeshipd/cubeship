import { templateJsonSchema } from "@/lib/template/jsonschema";

// The path is permanent: editors and agents point at it forever.
export const dynamic = "force-static";

export function GET() {
  return Response.json(templateJsonSchema(), {
    headers: { "cache-control": "public, max-age=3600", "access-control-allow-origin": "*" },
  });
}
