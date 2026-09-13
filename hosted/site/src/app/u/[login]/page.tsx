import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { TemplateCard } from "@/components/templates/card";
import { templatesByAuthor } from "@/lib/templates/queries";
import { findByLogin } from "@/lib/users";

// Reads the database, so this page can never be static.
export const dynamic = "force-dynamic";

export async function generateMetadata(props: PageProps<"/u/[login]">): Promise<Metadata> {
  const { login } = await props.params;
  const author = await findByLogin(login);
  if (!author) return {};
  return { title: author.login, alternates: { canonical: `/u/${author.login}` } };
}

export default async function AuthorPage(props: PageProps<"/u/[login]">) {
  const { login } = await props.params;
  const author = await findByLogin(login);
  if (!author) notFound();

  const templates = await templatesByAuthor(author.id, ["published"]);

  return (
    <div className="mx-auto max-w-6xl px-6 py-12">
      <div className="flex items-center gap-4">
        {author.avatarUrl ? (
          // biome-ignore lint/performance/noImgElement: an external GitHub avatar, not one of our own assets.
          <img src={author.avatarUrl} alt="" className="size-14 border border-fd-border" />
        ) : null}
        <div>
          <p className="label text-primary">Author</p>
          <h1 className="mt-1 font-semibold text-2xl text-fd-foreground tracking-tight">
            {author.name ?? author.login}
          </h1>
          <p className="text-fd-muted-foreground text-sm">@{author.login}</p>
        </div>
      </div>

      {templates.length === 0 ? (
        <p className="mt-12 text-fd-muted-foreground text-sm">Nothing published yet.</p>
      ) : (
        <div className="mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {templates.map((template) => (
            <TemplateCard key={template.slug} template={template} />
          ))}
        </div>
      )}
    </div>
  );
}
