import { Star } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { GitHubIcon } from "@/components/icons";
import { TemplateIcon } from "@/components/templates/card";
import { Preview } from "@/components/templates/preview";
import { Readme } from "@/components/templates/readme";
import { SourceBlock } from "@/components/templates/source-block";
import { avatarAt } from "@/lib/avatar";
import { formatDate } from "@/lib/dates";
import { releasesOf, templateByPath } from "@/lib/catalog";

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

const row = "text-right font-mono text-[0.6875rem] leading-5 text-fd-foreground";
const term = "label leading-5 text-fd-muted-foreground";

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
    <div className="mx-auto max-w-6xl px-6 py-12">
      <header className="flex items-center gap-4">
        <TemplateIcon src={found.icon_url} className="size-16" />
        <div className="min-w-0">
          <h1 className="font-semibold text-3xl text-fd-foreground tracking-tight">
            {found.title}
          </h1>
          <p className="mt-1 max-w-3xl text-fd-muted-foreground">{found.description}</p>
        </div>
      </header>

      <div className="mt-8 grid gap-10 lg:grid-cols-[minmax(0,1fr)_18rem]">
        {/* First in the markup so a phone sees who wrote it and which release this is
            before the README; beside the content, and following the reader, on a wide screen. */}
        <aside className="lg:sticky lg:top-24 lg:order-2 lg:self-start">
          <div className="hud-frame divide-y divide-fd-border border border-fd-border text-sm">
            <div className="flex items-center gap-3 p-4">
              {/* biome-ignore lint/performance/noImgElement: a GitHub avatar, not one of our own assets. */}
              <img
                src={avatarAt(found.avatar_url, 72)}
                alt=""
                width={36}
                height={36}
                className="size-9 shrink-0 border border-fd-border"
              />
              <div className="min-w-0 flex-1">
                <p className="label text-fd-muted-foreground">Author</p>
                <a
                  href={`https://github.com/${found.owner}`}
                  className="block truncate text-primary hover:text-glow"
                >
                  {found.owner}
                </a>
              </div>
              <a
                href={found.url}
                aria-label={`${found.owner}/${found.name} on GitHub`}
                className="text-fd-muted-foreground transition-colors hover:text-primary"
              >
                <GitHubIcon className="size-4" />
              </a>
            </div>

            <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-2 p-4">
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
              <div className="p-4">
                <p className="label mb-2 text-fd-muted-foreground">Tags</p>
                <div className="flex flex-wrap gap-2">
                  {found.tags.map((tag) => (
                    <Link
                      key={tag}
                      href={`/templates?tag=${encodeURIComponent(tag)}`}
                      className="label border border-fd-border px-2 py-1 text-fd-muted-foreground hover:border-primary hover:text-primary"
                    >
                      {tag}
                    </Link>
                  ))}
                </div>
              </div>
            ) : null}

            <div className="p-4">
              <p className="label mb-2 text-fd-muted-foreground">Releases</p>
              <ul className="space-y-1.5">
                {history.map((r) => (
                  <li key={r.tag} className="flex items-baseline justify-between gap-3">
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

        <div className="min-w-0 space-y-10 lg:order-1">
          {found.readme ? (
            <section className="hud-frame border border-fd-border p-6">
              <Readme
                source={found.readme}
                owner={found.owner}
                repo={found.name}
                commit={release.commit}
              />
            </section>
          ) : null}

          {manifest ? (
            <section>
              <p className="label mb-3 text-primary">What this creates</p>
              <Preview manifest={manifest} />
            </section>
          ) : null}

          {found.source ? (
            <section>
              <SourceBlock source={found.source} rawUrl={found.source_url} />
            </section>
          ) : null}
        </div>
      </div>
    </div>
  );
}
