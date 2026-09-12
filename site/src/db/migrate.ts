import { sql } from "drizzle-orm";
import { migrate } from "drizzle-orm/node-postgres/migrator";
import { db } from "./client";

// One arbitrary constant, held for the whole migration: two containers
// starting at once must not both apply the same file.
const LOCK = 4_212_001;

export async function runMigrations(): Promise<void> {
  const handle = db();
  await handle.execute(sql`select pg_advisory_lock(${LOCK})`);
  try {
    await migrate(handle, { migrationsFolder: "./drizzle" });
  } finally {
    await handle.execute(sql`select pg_advisory_unlock(${LOCK})`);
  }
}
