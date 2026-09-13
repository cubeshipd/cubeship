import { eq } from "drizzle-orm";
import { db } from "@/db/client";
import { templates } from "@/db/schema";

// A slug is permanent once published, so it is worth getting a readable
// one at creation and never touching it again.
export async function freeSlug(name: string): Promise<string> {
  const base =
    name
      .toLowerCase()
      .normalize("NFD")
      .replace(/[̀-ͯ]/g, "")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48) || "template";

  for (let attempt = 1; attempt < 100; attempt++) {
    const candidate = attempt === 1 ? base : `${base}-${attempt}`;
    const [taken] = await db()
      .select({ id: templates.id })
      .from(templates)
      .where(eq(templates.slug, candidate))
      .limit(1);
    if (!taken) return candidate;
  }

  throw new Error(`no free slug for "${name}"`);
}
