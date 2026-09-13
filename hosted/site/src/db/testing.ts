import { sql } from "drizzle-orm";
import { db } from "./client";
import { runMigrations } from "./migrate";

// Tests that need Postgres are skipped when there is none, the way the
// Go suite skips its DB tests with -short. CI always sets this.
export const testDatabaseUrl = process.env.SITE_TEST_DATABASE_URL;

export async function withDatabase(): Promise<ReturnType<typeof db>> {
  process.env.DATABASE_URL = testDatabaseUrl;
  await runMigrations();
  const handle = db();
  await handle.execute(
    sql`truncate reports, comments, likes, template_versions, templates, sessions, users restart identity cascade`,
  );
  return handle;
}
