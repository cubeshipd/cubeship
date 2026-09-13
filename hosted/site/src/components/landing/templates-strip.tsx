import Link from "next/link";
import { TemplateCard } from "@/components/templates/card";
import { withDeadline } from "@/lib/deadline";
import { listTemplates } from "@/lib/templates/queries";
import { Section } from "./section";

// The four most-starred templates, the same query and card the
// catalog itself uses. This is what makes the page read Postgres at
// request time — see the `force-dynamic` on the page that renders it.
// A database that is down must not take the landing page with it, so a
// failed query degrades to no section at all rather than an error.
export async function TemplatesStrip() {
  let rows: Awaited<ReturnType<typeof listTemplates>>["rows"];
  try {
    ({ rows } = await withDeadline(
      listTemplates({ sort: "stars", limit: 4 }),
      800,
      "templates strip",
    ));
  } catch (error) {
    console.warn("templates strip: catalog unavailable:", (error as Error).message);
    return null;
  }
  if (rows.length === 0) return null;

  return (
    <Section
      label="Templates"
      title="Apps the community already packaged"
      lede="An app and the managed data it needs, published as a GitHub repository by someone who already runs it."
    >
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {rows.map((template) => (
          <TemplateCard key={`${template.owner}/${template.name}`} template={template} />
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
