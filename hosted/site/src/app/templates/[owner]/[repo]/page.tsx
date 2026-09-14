import { Star } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { GitHubIcon } from "@/components/icons";
import { TemplateIcon } from "@/components/templates/card";
import { Preview } from "@/components/templates/preview";
import { Readme } from "@/components/templates/readme";
import { SourceBlock } from "@/components/templates/source-block";
import { VerifiedBadge } from "@/components/templates/verified";
import { avatarAt } from "@/lib/avatar";
import { releasesOf, templateByPath } from "@/lib/catalog";
import { formatDate } from "@/lib/dates";

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

const row = "template-info-value";
const term = "template-info-term";

export default async function TemplatePage(props: PageProps<"/templates/[owner]/[repo]">) {
  const { owner, repo } = await props.params;
  const found = await templateByPath(owner, repo);
  if (!found) notFound();

  const { release, manifest } = found;
  const published = new Date(release.published_at);
  const history = (await releasesOf(found.owner, found.name)).filter(
    (r) => r.status === "accepted",
  );

  return (
    <div className="site-container template-detail">
      <Link href="/templates" className="template-detail-back">
        <span aria-hidden>←</span> All templates
      </Link>
      <header className="template-detail-header">
        <TemplateIcon src={found.icon_url} className="template-detail-icon" />
        <div className="min-w-0 flex-1">
          <p className="template-detail-path">
            {found.owner}/{found.name}
          </p>
          <h1>
            {found.title}
            {found.verified ? <VerifiedBadge className="size-6" /> : null}
          </h1>
          <p className="template-detail-description">{found.description}</p>
        </div>
      </header>

      <div className="template-detail-grid">
        {/* First in the markup so a phone sees who wrote it and which release this is
            before the README; beside the content, and following the reader, on a wide screen. */}
        <aside className="template-detail-aside">
          <div className="template-info hud-frame">
            <div className="template-info-author">
              {/* biome-ignore lint/performance/noImgElement: a GitHub avatar, not one of our own assets. */}
              <img
                src={avatarAt(found.avatar_url, 72)}
                alt=""
                width={36}
                height={36}
                className="size-9 shrink-0 border border-fd-border"
              />
              <div className="min-w-0 flex-1">
                <p className="template-info-label">Author</p>
                <a href={`https://github.com/${found.owner}`} className="template-info-author-link">
                  {found.owner}
                </a>
              </div>
              <a
                href={found.url}
                aria-label={`${found.owner}/${found.name} on GitHub`}
                className="template-info-github"
              >
                <GitHubIcon className="size-4" />
              </a>
            </div>

            <dl className="template-info-facts">
              <dt className={term}>Stars</dt>
              <dd className={`${row} flex items-center justify-end gap-1`}>
                <Star className="size-3" aria-hidden />
                {found.stars}
              </dd>
              <dt className={term}>Updated</dt>
              <dd className={row}>
                <time dateTime={published.toISOString()}>{formatDate(published)}</time>
              </dd>
              <dt className={term}>Release</dt>
              <dd className={row}>
                <a href={release.url} className="hover:text-primary">
                  {release.tag}
                </a>
              </dd>
              {manifest?.min_cubeship ? (
                <>
                  <dt className={term}>Requires</dt>
                  <dd className={row}>Cubeship {manifest.min_cubeship}</dd>
                </>
              ) : null}
            </dl>

            {found.tags.length > 0 ? (
              <div className="template-info-section">
                <p className="template-info-label">Topics</p>
                <div className="template-info-tags">
                  {found.tags.map((tag) => (
                    <Link
                      key={tag}
                      href={`/templates?tag=${encodeURIComponent(tag)}`}
                      className="template-info-tag"
                    >
                      {tag}
                    </Link>
                  ))}
                </div>
              </div>
            ) : null}

            <div className="template-info-section">
              <p className="template-info-label">Release history</p>
              <ul className="template-release-list">
                {history.map((r) => (
                  <li key={r.tag}>
                    <a
                      href={r.url}
                      className="truncate font-mono text-fd-foreground hover:text-primary"
                    >
                      {r.tag}
                      {r.tag === release.tag ? (
                        <span className="ml-2 text-primary text-xs">current</span>
                      ) : null}
                    </a>
                    <time
                      dateTime={r.published_at}
                      className="shrink-0 text-fd-muted-foreground text-xs"
                    >
                      {formatDate(new Date(r.published_at))}
                    </time>
                  </li>
                ))}
              </ul>
            </div>
          </div>
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

          {manifest ? (
            <section className="template-detail-section">
              <p className="template-section-title">What this creates</p>
              <Preview manifest={manifest} />
            </section>
          ) : null}

          {found.source ? (
            <section className="template-detail-section">
              <SourceBlock
                source={found.source}
                fileUrl={`${found.url}/blob/${release.commit}/template.yaml`}
              />
            </section>
          ) : null}
        </div>
      </div>
    </div>
  );
}
