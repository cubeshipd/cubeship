import Link from "next/link";
import { TemplateCard } from "@/components/templates/card";
import { listTemplates } from "@/lib/templates/queries";
import { Section } from "./section";

// The four most-liked published templates, the same query and card the
// catalog itself uses. This is what makes the page read Postgres at
// request time — see the `force-dynamic` on the page that renders it.
export async function TemplatesStrip() {
  const { rows } = await listTemplates({ sort: "likes", limit: 4 });
  if (rows.length === 0) return null;

  return (
    <Section
      label="Templates"
      title="Recipes the community already wrote"
      lede="An app and the managed data it needs, published by someone who has already run it. Copy it, change what's yours, deploy."
    >
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {rows.map((template) => (
          <TemplateCard key={template.slug} template={template} />
        ))}
      </div>
      <div className="mt-8">
        <Link href="/templates" className="label text-primary hover:text-glow">
          Browse the catalog →
        </Link>
      </div>
    </Section>
  );
}
