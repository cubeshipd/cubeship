import type { Metadata } from "next";
import Link from "next/link";
import { TemplateCard } from "@/components/templates/card";
import { Filters } from "@/components/templates/filters";
import { allTemplates, distinctTags } from "@/lib/catalog";

// Reads the catalog on every request, so this page can never be static.
export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Templates",
  description: "Apps and the managed data they need, published as GitHub repositories.",
  alternates: { canonical: "/templates" },
};

function firstOf(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

export default async function TemplatesPage(props: PageProps<"/templates">) {
  const params = await props.searchParams;
  const q = firstOf(params.q);
  const tag = firstOf(params.tag);
  const sort = firstOf(params.sort) === "stars" ? "stars" : "recent";

  const [rows, tags] = await Promise.all([allTemplates({ q, tag, sort }), distinctTags()]);

  return (
    <div className="site-container templates-catalog">
      <header className="templates-hero">
        <div>
          <p className="section-kicker">Community templates</p>
          <h1>Your next app starts here.</h1>
        </div>
        <div className="templates-hero-copy">
          <p>
            Deploy complete projects with the apps and managed data they need, published and
            versioned on GitHub.
          </p>
          <Link href="/docs/templates/publishing" className="inline-link">
            Publish a template <span aria-hidden>↗</span>
          </Link>
        </div>
      </header>

      <div className="templates-filter-deck">
        <p className="templates-result-count">
          {rows.length === 1 ? "1 template" : `${rows.length} templates`}
          {q || tag ? " match this view" : " ready to explore"}
        </p>
        <Filters tags={tags} q={q} tag={tag} sort={sort} />
      </div>

      {rows.length === 0 ? (
        <div className="templates-empty hud-frame">
          {q || tag ? (
            <>
              <h2>No template matches this view.</h2>
              <p>Try another name or topic, or reset the catalog to see everything.</p>
              <Link href="/templates" className="inline-link">
                Clear the filters <span aria-hidden>↗</span>
              </Link>
            </>
          ) : (
            <>
              <h2>The catalog is ready for its first template.</h2>
              <p>Publish a GitHub release with the Cubeship topic to make it available here.</p>
              <Link href="/docs/templates/publishing" className="inline-link">
                Read the publishing guide <span aria-hidden>↗</span>
              </Link>
            </>
          )}
        </div>
      ) : (
        <>
          <div className="templates-grid">
            {rows.map((template) => (
              <TemplateCard key={`${template.owner}/${template.name}`} template={template} />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
