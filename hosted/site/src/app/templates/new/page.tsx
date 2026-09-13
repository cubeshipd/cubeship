import { redirect } from "next/navigation";
import { TemplateForm } from "@/components/templates/form";
import { currentUser } from "@/lib/auth/session";

// Reads the session, so this page can never be static.
export const dynamic = "force-dynamic";

export default async function NewTemplatePage() {
  const user = await currentUser();
  if (!user) redirect("/api/auth/github?next=/templates/new");

  return (
    <div className="mx-auto max-w-6xl px-6 py-12">
      <p className="label text-primary">New template</p>
      <h1 className="mt-2 font-semibold text-2xl text-fd-foreground tracking-tight">
        Write the file, publish version 1
      </h1>
      <div className="mt-8">
        <TemplateForm mode="new" />
      </div>
    </div>
  );
}
