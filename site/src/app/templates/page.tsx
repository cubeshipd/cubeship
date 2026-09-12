import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { TemplateCard } from "@/components/templates/card";
import { Filters } from "@/components/templates/filters";
import { distinctTags, listTemplates } from "@/lib/templates/queries";

// Reads Postgres on every request, so this page can never be static.
export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Templates",
  description: "Recipes for apps and the managed data they need, published by the community.",
  alternates: { canonical: "/templates" },
};

function firstOf(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

export default async function TemplatesPage(props: PageProps<"/templates">) {
  const params = await props.searchParams;
  const q = firstOf(params.q);
  const tag = firstOf(params.tag);
  const cursor = firstOf(params.cursor);
  const sort = firstOf(params.sort) === "likes" ? "likes" : "recent";

  const [{ rows, nextCursor }, tags] = await Promise.all([
    listTemplates({ q, tag, sort, cursor }),
    distinctTags(),
  ]);

  const nextParams = new URLSearchParams();
  if (q) nextParams.set("q", q);
  if (tag) nextParams.set("tag", tag);
  if (sort !== "recent") nextParams.set("sort", sort);
  if (nextCursor) nextParams.set("cursor", nextCursor);

  return (
    <div className="mx-auto max-w-6xl px-6 py-12">
      <p className="label text-primary">Templates</p>
      <h1 className="mt-2 font-semibold text-2xl text-fd-foreground tracking-tight">
        A recipe for the apps and the managed data they need
      </h1>

      <div className="mt-8">
        <Suspense>
          <Filters tags={tags} />
        </Suspense>
      </div>

      {rows.length === 0 ? (
        <div className="hud-frame mt-12 border border-fd-border p-8 text-center">
          <p className="text-fd-muted-foreground text-sm">Nothing is published yet.</p>
          <Link href="/templates/new" className="mt-3 inline-block text-primary text-sm">
            Write the first one →
          </Link>
        </div>
      ) : (
        <>
          <div className="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {rows.map((template) => (
              <TemplateCard key={template.slug} template={template} />
            ))}
          </div>
          {nextCursor ? (
            <div className="mt-8 text-center">
              <Link
                href={`/templates?${nextParams}`}
                className="label border border-fd-border px-4 py-2 text-fd-muted-foreground hover:border-primary hover:text-primary"
              >
                More →
              </Link>
            </div>
          ) : null}
        </>
      )}
    </div>
  );
}
