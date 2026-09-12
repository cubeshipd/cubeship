import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { Comments } from "@/components/templates/comments";
import { LikeButton } from "@/components/templates/like-button";
import { Preview } from "@/components/templates/preview";
import { ReportButton } from "@/components/templates/report-button";
import { SourceBlock } from "@/components/templates/source-block";
import { currentUser } from "@/lib/auth/session";
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
    <div className="mx-auto max-w-4xl px-6 py-12">
      {template.imageKey ? (
        <div className="aspect-video overflow-hidden border border-fd-border bg-grid">
          {/* biome-ignore lint/performance/noImgElement: served from our own path, re-encoded on upload — not a remote domain next/image needs configured for. */}
          <img src={`/i/${template.imageKey}`} alt="" className="h-full w-full object-cover" />
        </div>
      ) : null}

      <div className="mt-6">
        <h1 className="font-semibold text-2xl text-fd-foreground tracking-tight">
          {template.name}
        </h1>
        <p className="mt-2 max-w-2xl text-fd-muted-foreground">{template.summary}</p>
        <p className="mt-2 text-fd-muted-foreground text-sm">
          by{" "}
          <Link href={`/u/${author.login}`} className="text-primary hover:text-glow">
            {author.login}
          </Link>
        </p>
        <div className="mt-4 flex items-center gap-4">
          <LikeButton slug={slug} initialCount={template.likesCount} initialLiked={liked} />
          <ReportButton subjectType="template" subjectId={template.id} />
        </div>
      </div>

      {template.tags.length > 0 ? (
        <div className="mt-4 flex flex-wrap gap-2">
          {template.tags.map((tag) => (
            <span
              key={tag}
              className="label border border-fd-border px-2 py-1 text-fd-muted-foreground"
            >
              {tag}
            </span>
          ))}
        </div>
      ) : null}

      {manifest ? (
        <div className="mt-10">
          <p className="label mb-3 text-primary">What this creates</p>
          <Preview manifest={manifest} />
        </div>
      ) : null}

      {version ? (
        <div className="mt-10">
          <SourceBlock source={version.source} manifestUrl={manifestUrl} />
        </div>
      ) : null}

      {versions.length > 0 ? (
        <div className="mt-10">
          <p className="label mb-3 text-primary">Versions</p>
          <ul className="hud-frame divide-y divide-fd-border border border-fd-border text-sm">
            {versions.map((v) => (
              <li key={v.number} className="flex items-center justify-between px-4 py-2">
                <Link
                  href={`/templates/${slug}/versions/${v.number}`}
                  className="text-fd-foreground hover:text-primary"
                >
                  Version {v.number}
                  {v.number === version?.number ? (
                    <span className="ml-2 text-primary text-xs">current</span>
                  ) : null}
                </Link>
                {v.notes ? (
                  <span className="text-fd-muted-foreground text-xs">{v.notes}</span>
                ) : null}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <div className="mt-10">
        <p className="label mb-3 text-primary">Comments</p>
        <Comments
          slug={slug}
          initialComments={comments.map((comment) => ({
            ...comment,
            createdAt: comment.createdAt.toISOString(),
          }))}
          viewer={viewer ? { login: viewer.login, isAdmin: viewer.role === "admin" } : null}
        />
      </div>
    </div>
  );
}
