import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { GitHubIcon } from "@/components/icons";
import { Comments } from "@/components/templates/comments";
import { LikeButton } from "@/components/templates/like-button";
import { Preview } from "@/components/templates/preview";
import { SourceBlock } from "@/components/templates/source-block";
import { currentUser } from "@/lib/auth/session";
import { avatarAt } from "@/lib/avatar";
import { formatDate } from "@/lib/dates";
import { publicUrl } from "@/lib/env";
import type { NormalizedManifest } from "@/lib/template";
import { currentVersion, templateWithAuthor, versionsOf, visibleTo } from "@/lib/templates/queries";
import { hasLiked, listComments } from "@/lib/templates/social";

// Reads the session and the database, so this page can never be static.
export const dynamic = "force-dynamic";

export async function generateMetadata(props: PageProps<"/templates/[slug]">): Promise<Metadata> {
  const { slug } = await props.params;
  const [row, viewer] = await Promise.all([templateWithAuthor(slug), currentUser()]);
  if (!row || !visibleTo(row.template, viewer)) return {};

  return {
    title: row.template.name,
    description: row.template.summary,
    alternates: { canonical: `/templates/${slug}` },
    openGraph: {
      images: [row.template.imageKey ? `/i/${row.template.imageKey}` : `/og/templates/${slug}`],
    },
  };
}

export default async function TemplateDetailPage(props: PageProps<"/templates/[slug]">) {
  const { slug } = await props.params;
  const [row, viewer] = await Promise.all([templateWithAuthor(slug), currentUser()]);
  if (!row || !visibleTo(row.template, viewer)) notFound();

  const { template, author } = row;
  const [version, versions, liked, comments] = await Promise.all([
    currentVersion(template.id),
    versionsOf(template.id),
    viewer ? hasLiked(viewer.id, template.id) : Promise.resolve(false),
    listComments(template.id),
  ]);

  // Stored as jsonb, so the column carries no static type of its own.
  const manifest = version?.manifest as NormalizedManifest | undefined;
  const manifestUrl = `${publicUrl()}/api/v1/templates/${slug}/manifest`;

  return (
    <div className="mx-auto max-w-6xl px-6 py-12">
      <header>
        <h1 className="font-semibold text-3xl text-fd-foreground tracking-tight">
          {template.name}
        </h1>
        <p className="mt-2 max-w-3xl text-fd-muted-foreground">{template.summary}</p>
      </header>

      <div className="mt-8 grid gap-10 lg:grid-cols-[minmax(0,1fr)_18rem]">
        {/* First in the markup so a phone sees who wrote it and what version it is
            before the file; beside the content, and following the reader, on a wide screen. */}
        <aside className="lg:sticky lg:top-24 lg:order-2 lg:self-start">
          <div className="hud-frame divide-y divide-fd-border border border-fd-border text-sm">
            <div className="flex items-center gap-3 p-4">
              {author.avatarUrl ? (
                // biome-ignore lint/performance/noImgElement: a GitHub avatar, not one of our own assets.
                <img
                  src={avatarAt(author.avatarUrl, 72)}
                  alt=""
                  width={36}
                  height={36}
                  className="size-9 shrink-0 border border-fd-border"
                />
              ) : null}
              <div className="min-w-0 flex-1">
                <p className="label text-fd-muted-foreground">Author</p>
                <Link
                  href={`/u/${author.login}`}
                  className="block truncate text-primary hover:text-glow"
                >
                  {author.login}
                </Link>
              </div>
              <LikeButton slug={slug} initialCount={template.likesCount} initialLiked={liked} />
              <a
                href={`https://github.com/${author.login}`}
                target="_blank"
                rel="noreferrer"
                aria-label={`${author.login} on GitHub`}
                className="text-fd-muted-foreground transition-colors hover:text-primary"
              >
                <GitHubIcon className="size-4" />
              </a>
            </div>

            <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-2 p-4">
              <dt className="label leading-5 text-fd-muted-foreground">Updated</dt>
              <dd className="text-right font-mono text-[0.6875rem] leading-5 text-fd-foreground">
                <time dateTime={template.updatedAt.toISOString()}>
                  {formatDate(template.updatedAt)}
                </time>
              </dd>
              <dt className="label leading-5 text-fd-muted-foreground">Published</dt>
              <dd className="text-right font-mono text-[0.6875rem] leading-5 text-fd-foreground">
                <time dateTime={template.createdAt.toISOString()}>
                  {formatDate(template.createdAt)}
                </time>
              </dd>
              {version ? (
                <>
                  <dt className="label leading-5 text-fd-muted-foreground">Version</dt>
                  <dd className="text-right font-mono text-[0.6875rem] leading-5 text-fd-foreground">
                    v{version.number}
                  </dd>
                </>
              ) : null}
              {manifest?.min_cubeship ? (
                <>
                  <dt className="label leading-5 text-fd-muted-foreground">Requires</dt>
                  <dd className="text-right font-mono text-[0.6875rem] leading-5 text-fd-foreground">
                    Cubeship {manifest.min_cubeship}
                  </dd>
                </>
              ) : null}
            </dl>

            {template.tags.length > 0 ? (
              <div className="p-4">
                <p className="label mb-2 text-fd-muted-foreground">Tags</p>
                <div className="flex flex-wrap gap-2">
                  {template.tags.map((tag) => (
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

            {versions.length > 0 ? (
              <div className="p-4">
                <p className="label mb-2 text-fd-muted-foreground">Versions</p>
                <ul className="space-y-1.5">
                  {versions.map((v) => (
                    <li key={v.number} className="flex items-baseline justify-between gap-3">
                      <Link
                        href={`/templates/${slug}/versions/${v.number}`}
                        className="font-mono text-fd-foreground hover:text-primary"
                      >
                        v{v.number}
                        {v.number === version?.number ? (
                          <span className="ml-2 text-primary text-xs">current</span>
                        ) : null}
                      </Link>
                      <time
                        dateTime={v.createdAt.toISOString()}
                        className="shrink-0 text-fd-muted-foreground text-xs"
                      >
                        {formatDate(v.createdAt)}
                      </time>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </div>
        </aside>

        <div className="min-w-0 space-y-10 lg:order-1">
          {template.imageKey ? (
            <div className="aspect-video overflow-hidden border border-fd-border bg-grid">
              {/* biome-ignore lint/performance/noImgElement: served from our own path, re-encoded on upload — not a remote domain next/image needs configured for. */}
              <img src={`/i/${template.imageKey}`} alt="" className="h-full w-full object-cover" />
            </div>
          ) : null}

          {manifest ? (
            <section>
              <p className="label mb-3 text-primary">What this creates</p>
              <Preview manifest={manifest} />
            </section>
          ) : null}

          {version ? (
            <section>
              <SourceBlock source={version.source} manifestUrl={manifestUrl} />
            </section>
          ) : null}

          <section>
            <p className="label mb-3 text-primary">Comments</p>
            <Comments
              slug={slug}
              initialComments={comments.map((comment) => ({
                ...comment,
                createdAt: comment.createdAt.toISOString(),
              }))}
              viewer={viewer ? { login: viewer.login, isAdmin: viewer.role === "admin" } : null}
            />
          </section>
        </div>
      </div>
    </div>
  );
}
