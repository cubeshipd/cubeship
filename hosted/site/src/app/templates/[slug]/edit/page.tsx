import { notFound } from "next/navigation";
import { TemplateForm } from "@/components/templates/form";
import { currentUser } from "@/lib/auth/session";
import { currentVersion, templateWithAuthor } from "@/lib/templates/queries";

// Reads the session and the database, so this page can never be static.
export const dynamic = "force-dynamic";

export default async function EditTemplatePage(props: PageProps<"/templates/[slug]/edit">) {
  const { slug } = await props.params;
  const [user, row] = await Promise.all([currentUser(), templateWithAuthor(slug)]);
  // A template that is not the caller's own is refused the same way one
  // that does not exist is: nothing about who owns it leaks either way.
  if (!row || !user || (user.id !== row.template.authorId && user.role !== "admin")) notFound();

  const version = await currentVersion(row.template.id);

  return (
    <div className="mx-auto max-w-6xl px-6 py-12">
      <p className="label text-primary">Editing</p>
      <h1 className="mt-2 font-semibold text-2xl text-fd-foreground tracking-tight">
        {row.template.name}
      </h1>
      <div className="mt-8">
        <TemplateForm
          mode="edit"
          template={{
            slug: row.template.slug,
            name: row.template.name,
            summary: row.template.summary,
            tags: row.template.tags,
            imageKey: row.template.imageKey,
            source: version?.source ?? "",
          }}
        />
      </div>
    </div>
  );
}
