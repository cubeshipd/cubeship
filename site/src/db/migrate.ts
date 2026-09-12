import { sql } from "drizzle-orm";
import { migrate } from "drizzle-orm/node-postgres/migrator";
import { db } from "./client";

// One arbitrary constant, held for the whole migration: two containers
// starting at once must not both apply the same file.
const LOCK = 4_212_001;

// A single attempt, and the real error if it fails. withDatabase() in
// tests awaits this directly against a database that is already up, so
// retrying here would only add delay nobody wants; the retry loop lives
// in runMigrationsInBackground below.
export async function runMigrations(): Promise<void> {
  const handle = db();
  await handle.execute(sql`select pg_advisory_lock(${LOCK})`);
  try {
    await migrate(handle, { migrationsFolder: "./drizzle" });
  } finally {
    await handle.execute(sql`select pg_advisory_unlock(${LOCK})`);
  }
}

const INITIAL_BACKOFF_MS = 1_000;
const MAX_BACKOFF_MS = 30_000;

// Startup must never wait on a database that might not be reachable
// yet, so this schedules attempts instead of blocking register(): log
// one line, back off, and try again, doubling the wait up to
// MAX_BACKOFF_MS until one attempt applies cleanly. It never throws —
// there is nobody left in register() to catch it.
export function runMigrationsInBackground(): void {
  let delay = INITIAL_BACKOFF_MS;
  const attempt = () => {
    runMigrations().then(
      () => console.log("migrations applied"),
      (error) => {
        console.error("migrations failed, retrying:", (error as Error).message);
        setTimeout(attempt, delay);
        delay = Math.min(delay * 2, MAX_BACKOFF_MS);
      },
    );
  };
  attempt();
}
