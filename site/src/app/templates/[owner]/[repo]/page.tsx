import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { TemplateIcon } from "@/components/templates/card";
import { Preview } from "@/components/templates/preview";
import { Readme } from "@/components/templates/readme";
import { SourceBlock } from "@/components/templates/source-block";
import { VerifiedBadge } from "@/components/templates/verified";
import { templateByPath } from "@/lib/catalog";

// Reads the catalog, so this page can never be static.
export const dynamic = "force-dynamic";

export async function generateMetadata(
  props: PageProps<"/templates/[owner]/[repo]">,
): Promise<Metadata> {
  const { owner, repo } = await props.params;
  const found = await templateByPath(owner, repo);
  if (!found) return {};

  const path = `/templates/${found.owner}/${found.name}`;
  return {
    title: found.title,
    description: found.description,
    alternates: { canonical: path },
    openGraph: { images: [`/og${path}`] },
  };
}

export default async function TemplatePage(props: PageProps<"/templates/[owner]/[repo]">) {
  const { owner, repo } = await props.params;
  const found = await templateByPath(owner, repo);
  if (!found) notFound();

  const { release, manifest } = found;

  return (
    <div className="site-container template-detail">
      <Link href="/templates" className="template-detail-back">
        <span aria-hidden>←</span> All templates
      </Link>
      <header className="template-detail-header">
        <TemplateIcon src={found.icon_url} className="template-detail-icon" />
        <div className="min-w-0 flex-1">
          <h1>
            {found.title}
            {found.verified ? <VerifiedBadge className="size-6" /> : null}
          </h1>
          <p className="template-detail-description">{found.description}</p>
        </div>
      </header>

      <div className="template-detail-grid">
        <aside className="template-detail-aside">
          {manifest ? (
            <section className="template-detail-section">
              <p className="template-section-title">What this creates</p>
              <Preview manifest={manifest} />
            </section>
          ) : null}
        </aside>

        <div className="template-detail-main">
          {found.readme ? (
            <section className="template-readme hud-frame">
              <Readme
                source={found.readme}
                owner={found.owner}
                repo={found.name}
                commit={release.commit}
              />
            </section>
          ) : null}

          {found.source ? (
            <section className="template-detail-section">
              <SourceBlock source={found.source} fileUrl={found.source_url} />
            </section>
          ) : null}
        </div>
      </div>
    </div>
  );
}
