// Next calls register() once when the server starts, which is the only
// hook that runs in the standalone image without shipping a second
// entrypoint script and its own node_modules.
export async function register() {
  if (process.env.NEXT_RUNTIME !== "nodejs") return;
  if (process.env.NEXT_PHASE === "phase-production-build") return;
  if (!process.env.DATABASE_URL) {
    console.warn("DATABASE_URL is not set: skipping migrations");
    return;
  }

  // Not awaited: a database that is down must not stop the server from
  // starting. runMigrationsInBackground retries on its own until one
  // attempt applies cleanly.
  const { runMigrationsInBackground } = await import("@/db/migrate");
  runMigrationsInBackground();
}
