import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { Preview } from "@/components/templates/preview";
import { SourceBlock } from "@/components/templates/source-block";
import { currentUser } from "@/lib/auth/session";
import { publicUrl } from "@/lib/env";
import type { NormalizedManifest } from "@/lib/template";
import { templateBySlug, versionOf, visibleTo } from "@/lib/templates/queries";

// Reads the session and the database, so this page can never be static.
export const dynamic = "force-dynamic";

export async function generateMetadata(
  props: PageProps<"/templates/[slug]/versions/[number]">,
): Promise<Metadata> {
  const { slug, number } = await props.params;
  return {
    title: `Version ${number}`,
    alternates: { canonical: `/templates/${slug}/versions/${number}` },
  };
}

export default async function TemplateVersionPage(
  props: PageProps<"/templates/[slug]/versions/[number]">,
) {
  const { slug, number } = await props.params;
  const parsed = Number(number);
  const [template, viewer] = await Promise.all([templateBySlug(slug), currentUser()]);
  if (!template || !visibleTo(template, viewer) || !Number.isInteger(parsed)) notFound();

  const version = await versionOf(template.id, parsed);
  if (!version) notFound();

  const manifest = version.manifest as NormalizedManifest;
  const manifestUrl = `${publicUrl()}/api/v1/templates/${slug}/manifest?version=${version.number}`;

  return (
    <div className="mx-auto max-w-4xl px-6 py-12">
      <p className="label text-primary">
        <Link href={`/templates/${slug}`} className="hover:text-glow">
          {template.name}
        </Link>
      </p>
      <h1 className="mt-2 font-semibold text-2xl text-fd-foreground tracking-tight">
        Version {version.number}
      </h1>
      {version.notes ? <p className="mt-2 text-fd-muted-foreground">{version.notes}</p> : null}

      <div className="mt-8">
        <p className="label mb-3 text-primary">What this creates</p>
        <Preview manifest={manifest} />
      </div>

      <div className="mt-10">
        <SourceBlock source={version.source} manifestUrl={manifestUrl} />
      </div>
    </div>
  );
}
