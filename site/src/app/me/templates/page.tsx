import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { currentUser } from "@/lib/auth/session";
import { templatesByAuthor } from "@/lib/templates/queries";

// Reads the session and the database, so this page can never be static.
export const dynamic = "force-dynamic";

export const metadata: Metadata = { title: "Your templates" };

export default async function MyTemplatesPage() {
  const user = await currentUser();
  if (!user) redirect("/api/auth/github?next=/me/templates");

  const templates = await templatesByAuthor(user.id, ["draft", "published", "unlisted"]);

  return (
    <div className="mx-auto max-w-4xl px-6 py-12">
      <p className="label text-primary">Your templates</p>
      <h1 className="mt-2 font-semibold text-2xl text-fd-foreground tracking-tight">
        Drafts, published, and anything unlisted
      </h1>

      <div className="mt-8">
        <Link
          href="/templates/new"
          className="label border border-fd-border px-3 py-2 text-fd-muted-foreground hover:border-primary hover:text-primary"
        >
          Write a new one
        </Link>
      </div>

      {templates.length === 0 ? (
        <p className="mt-8 text-fd-muted-foreground text-sm">Nothing yet.</p>
      ) : (
        <ul className="hud-frame mt-8 divide-y divide-fd-border border border-fd-border">
          {templates.map((template) => (
            <li key={template.slug} className="flex items-center justify-between px-4 py-3">
              <div>
                <Link
                  href={`/templates/${template.slug}/edit`}
                  className="text-fd-foreground hover:text-primary"
                >
                  {template.name}
                </Link>
                <p className="text-fd-muted-foreground text-xs">{template.summary}</p>
              </div>
              <span className="label text-subtle-foreground">{template.status}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
