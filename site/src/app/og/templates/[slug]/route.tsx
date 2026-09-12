import { generateOGImage } from "fumadocs-ui/og";
import { notFound } from "next/navigation";
import { appName } from "@/lib/shared";
import { templateBySlug, visibleTo } from "@/lib/templates/queries";

// Reads Postgres, so unlike the docs' own OG route this can never be
// prerendered — there is no set of slugs to hand generateStaticParams
// that would not need the database at build time.
export const dynamic = "force-dynamic";

export async function GET(_req: Request, { params }: RouteContext<"/og/templates/[slug]">) {
  const { slug } = await params;
  const template = await templateBySlug(slug);
  // No viewer here: a draft is nobody's business but its author's, and
  // an anonymous request behind an <img> tag can never be one.
  if (!template || !visibleTo(template, null)) notFound();

  return generateOGImage({
    title: template.name,
    description: template.summary,
    site: appName,
  });
}
