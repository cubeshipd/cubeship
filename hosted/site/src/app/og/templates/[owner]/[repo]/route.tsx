import { generateOGImage } from "fumadocs-ui/og";
import { notFound } from "next/navigation";
import { templateByPath } from "@/lib/catalog";
import { appName } from "@/lib/shared";

// Reads the catalog, so unlike the docs' own OG route this can never be
// prerendered: there is no set of templates known at build time.
export const dynamic = "force-dynamic";

export async function GET(_req: Request, { params }: RouteContext<"/og/templates/[owner]/[repo]">) {
  const { owner, repo } = await params;
  const found = await templateByPath(owner, repo);
  if (!found) notFound();

  return generateOGImage({
    title: found.title,
    description: found.description,
    site: appName,
  });
}
