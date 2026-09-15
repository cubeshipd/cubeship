import { ArrowUpRight } from "lucide-react";
import Link from "next/link";
import { TemplateCard } from "@/components/templates/card";
import { listTemplates } from "@/lib/catalog";
import { withDeadline } from "@/lib/deadline";

// The four most-starred templates, the same query and card the
// catalog itself uses, read from the catalog at request time — see the
// `force-dynamic` on the page that renders it. A catalog that is down
// must not take the landing page with it, so a failed read degrades to
// no section at all rather than an error.
export async function TemplatesStrip() {
  let rows: Awaited<ReturnType<typeof listTemplates>>["templates"];
  try {
    ({ templates: rows } = await withDeadline(
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
    <section className="site-container catalog-strip">
      <div className="section-heading">
        <div>
          <p className="section-kicker">A head start, included</p>
          <h2>
            Good software.
            <br />
            Ready for your server.
          </h2>
        </div>
        <div>
          <p className="section-description">
            Community templates package the app and the data it needs. Choose one and make it yours.
          </p>
          <Link href="/templates" className="inline-link">
            Explore the catalog <ArrowUpRight size={16} />
          </Link>
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {rows.map((template) => (
          <TemplateCard key={`${template.owner}/${template.name}`} template={template} />
        ))}
      </div>
    </section>
  );
}
