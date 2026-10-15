import type { Metadata } from "next";
import Link from "next/link";
import { TemplateCard } from "@/components/templates/card";
import { Filters } from "@/components/templates/filters";
import { allTemplates } from "@/lib/catalog";

// Reads the catalog on every request, so this page can never be static.
export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Templates",
  description: "Apps and the managed data they need, maintained in cubeship-templates.",
  alternates: { canonical: "/templates" },
};

function firstOf(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

export default async function TemplatesPage(props: PageProps<"/templates">) {
  const params = await props.searchParams;
  const q = firstOf(params.q);
  const rows = await allTemplates({ q });

  return (
    <div className="site-container templates-catalog">
      <header className="templates-hero">
        <div>
          <p className="section-kicker">Cubeship templates</p>
          <h1>Your next app starts here.</h1>
        </div>
        <div className="templates-hero-copy">
          <p>
            Deploy complete projects with the apps and managed data they need, maintained by
            Cubeship.
          </p>
        </div>
      </header>

      <div className="templates-filter-deck">
        <p className="templates-result-count">
          {rows.length === 1 ? "1 template" : `${rows.length} templates`}
          {q ? " match this view" : " ready to explore"}
        </p>
        <Filters q={q} />
      </div>

      {rows.length === 0 ? (
        <div className="templates-empty hud-frame">
          {q ? (
            <>
              <h2>No template matches this view.</h2>
              <p>Try another name, or reset the catalog to see everything.</p>
              <Link href="/templates" className="inline-link">
                Clear the filters <span aria-hidden>↗</span>
              </Link>
            </>
          ) : (
            <>
              <h2>The catalog is ready for its first template.</h2>
              <p>
                Add a template through the cubeship-templates repository to make it available here.
              </p>
              <Link href="/docs/templates/publishing" className="inline-link">
                Read the publishing guide <span aria-hidden>↗</span>
              </Link>
            </>
          )}
        </div>
      ) : (
        <div className="templates-grid">
          {rows.map((template) => (
            <TemplateCard key={`${template.owner}/${template.name}`} template={template} />
          ))}
        </div>
      )}
    </div>
  );
}
